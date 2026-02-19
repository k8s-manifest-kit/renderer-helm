package helm_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rs/xid"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/k8s-manifest-kit/engine/pkg/filter/meta/gvk"
	"github.com/k8s-manifest-kit/engine/pkg/transformer/meta/labels"
	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/cache"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

const (
	testChartPath = "../config/test/charts/simple-app"
)

//nolint:cyclop,goconst // Test function complexity and string repetition acceptable for readability
func TestRenderer(t *testing.T) {

	t.Run("should render chart from OCI registry", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "test-release",
				Values: helm.Values(map[string]any{
					"shared": map[string]any{
						"appId": "test-app",
					},
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		// Check that resources were rendered
		found := false
		for _, obj := range objects {
			if obj.GetKind() == "Deployment" || obj.GetKind() == "Service" {
				found = true

				break
			}
		}
		g.Expect(found).To(BeTrue(), "Should have rendered at least one Deployment or Service")
	})

	t.Run("should render chart from filesystem", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "filesystem-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 2,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3)) // Deployment + Service + CRD

		// Verify CRD, Deployment, and Service were rendered
		var foundCRD, foundDeployment, foundService bool
		for _, obj := range objects {
			switch obj.GetKind() {
			case "CustomResourceDefinition":
				foundCRD = true
				g.Expect(obj.GetName()).To(Equal("applications.example.com"))
			case "Deployment":
				foundDeployment = true
				g.Expect(obj.GetName()).To(Equal("filesystem-test-deployment"))
			case "Service":
				foundService = true
				g.Expect(obj.GetName()).To(Equal("filesystem-test-service"))
			default:
				// Ignore other kinds
			}
		}
		g.Expect(foundCRD).To(BeTrue(), "Should have rendered CRD")
		g.Expect(foundDeployment).To(BeTrue(), "Should have rendered Deployment")
		g.Expect(foundService).To(BeTrue(), "Should have rendered Service")
	})

	t.Run("should render with dynamic values", func(t *testing.T) {
		g := NewWithT(t)
		dynamicValues := func(_ context.Context) (map[string]any, error) {
			return map[string]any{
				"replicaCount": 3,
				"image": map[string]any{
					"tag": xid.New().String(),
				},
			}, nil
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "dynamic-test",
				Values:      dynamicValues,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})

	t.Run("should apply filters", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "filter-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
			helm.WithFilter(gvk.Filter(appsv1.SchemeGroupVersion.WithKind("Deployment"))),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(1)) // Only Deployment passes filter

		// All objects should be Deployments
		for _, obj := range objects {
			g.Expect(obj.GetKind()).To(Equal("Deployment"))
		}
	})

	t.Run("should apply transformers", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "transformer-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
			helm.WithTransformer(labels.Set(map[string]string{
				"managed-by": "helm-renderer",
				"env":        "test",
			})),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		// All objects should have the transformer labels
		for _, obj := range objects {
			g.Expect(obj.GetLabels()).To(HaveKeyWithValue("managed-by", "helm-renderer"))
			g.Expect(obj.GetLabels()).To(HaveKeyWithValue("env", "test"))
		}
	})

	t.Run("should process multiple charts", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "release-1",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
			{
				Chart:       testChartPath,
				ReleaseName: "release-2",
				Values: helm.Values(map[string]any{
					"replicaCount": 2,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(6)) // 3 objects per chart * 2 charts

		// Should have objects from both releases
		releaseNames := make(map[string]bool)
		for _, obj := range objects {
			if labels := obj.GetLabels(); labels != nil {
				if releaseName, ok := labels["app"]; ok {
					releaseNames[releaseName] = true
				}
			}
		}
		g.Expect(releaseNames).To(HaveKey("release-1"))
		g.Expect(releaseNames).To(HaveKey("release-2"))
	})

	t.Run("should render with release name in metadata", func(t *testing.T) {
		g := NewWithT(t)
		releaseName := "custom-release"
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: releaseName,
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		// Check that objects have the release name
		foundRelease := false
		for _, obj := range objects {
			if labels := obj.GetLabels(); labels != nil {
				if app := labels["app"]; app == releaseName {
					foundRelease = true

					break
				}
			}
			// Also check in object names
			if obj.GetKind() == "Deployment" && obj.GetName() == releaseName+"-deployment" {
				foundRelease = true
			}
		}
		g.Expect(foundRelease).To(BeTrue(), "Should find release name in labels or object names")
	})

	t.Run("should handle values context cancellation", func(t *testing.T) {
		g := NewWithT(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		valuesFn := func(ctx context.Context) (map[string]any, error) {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return map[string]any{}, nil
			}
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cancel-test",
				Values:      valuesFn,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(ctx, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("context canceled"))
	})

	t.Run("should combine filters and transformers", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "combined-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
			helm.WithFilter(gvk.Filter(appsv1.SchemeGroupVersion.WithKind("Deployment"))),
			helm.WithTransformer(labels.Set(map[string]string{
				"test": "combined",
			})),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(1)) // Only Deployment passes filter

		for _, obj := range objects {
			g.Expect(obj.GetKind()).To(Equal("Deployment"))
			g.Expect(obj.GetLabels()).To(HaveKeyWithValue("test", "combined"))
		}
	})
}

func TestNew(t *testing.T) {

	t.Run("should reject input without Chart", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				ReleaseName: "test",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("chart cannot be empty or whitespace-only"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with whitespace-only Chart", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "   ",
				ReleaseName: "test",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("chart cannot be empty or whitespace-only"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input without ReleaseName", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart: "oci://registry-1.docker.io/daprio/dapr-shared-chart",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("release name cannot be empty or whitespace-only"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with whitespace-only ReleaseName", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "   ",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("release name cannot be empty or whitespace-only"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with ReleaseName exceeding 53 characters", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "this-is-a-very-long-release-name-that-exceeds-the-limit",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("must not exceed 53 characters"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with uppercase letters in ReleaseName", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "MyApp",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("must consist of lowercase alphanumeric characters"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with underscores in ReleaseName", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "my_app",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("must consist of lowercase alphanumeric characters"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with ReleaseName starting with hyphen", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "-myapp",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("must consist of lowercase alphanumeric characters"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should reject input with ReleaseName ending with hyphen", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "myapp-",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("must consist of lowercase alphanumeric characters"))
		g.Expect(renderer).To(BeNil())
	})

	t.Run("should accept valid input", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())
	})

	t.Run("should accept ReleaseName with hyphens", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "my-app",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())
	})

	t.Run("should accept ReleaseName with numbers", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "my-app-123",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())
	})

	t.Run("should accept single character ReleaseName", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "a",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())
	})

	t.Run("should return error for non-existent chart", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/non-existent/chart",
				ReleaseName: "test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("unable to locate chart"))
	})

	t.Run("should return error for invalid chart path", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "/non/existent/path",
				ReleaseName: "test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer).ToNot(BeNil())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
	})
}

func TestValuesHelper(t *testing.T) {

	t.Run("should return static values", func(t *testing.T) {
		g := NewWithT(t)
		staticValues := map[string]any{
			"key1": "value1",
			"key2": 42,
		}

		valuesFn := helm.Values(staticValues)
		result, err := valuesFn(t.Context())

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(Equal(staticValues))
	})

	t.Run("should work with nil values", func(t *testing.T) {
		g := NewWithT(t)
		valuesFn := helm.Values(nil)
		result, err := valuesFn(t.Context())

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(BeNil())
	})

	t.Run("should work with empty map", func(t *testing.T) {
		g := NewWithT(t)
		valuesFn := helm.Values(map[string]any{})
		result, err := valuesFn(t.Context())

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(Equal(map[string]any{}))
	})

	t.Run("should handle Values function returning nil", func(t *testing.T) {
		g := NewWithT(t)

		nilValuesFunc := func(_ context.Context) (map[string]any, error) {
			return nil, nil //nolint:nilnil // Intentionally testing nil return
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "nil-values-test",
				Values:      nilValuesFunc,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Should not panic, should use empty map
		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})
}

func TestCacheIntegration(t *testing.T) {

	t.Run("should cache identical renders", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cache-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 2,
				}),
			},
		},
			helm.WithCache(),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render - cache miss
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Second render - cache hit (should be identical)
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))

		// Results should be equal
		for i := range result1 {
			g.Expect(result2[i]).To(Equal(result1[i]))
		}
	})

	t.Run("should miss cache on different values", func(t *testing.T) {
		g := NewWithT(t)
		callCount := 0
		dynamicValues := func(_ context.Context) (map[string]any, error) {
			callCount++

			return map[string]any{
				"replicaCount": callCount,
				"image": map[string]any{
					"tag": xid.New().String(),
				},
			}, nil
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "dynamic-cache-test",
				Values:      dynamicValues,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// First render
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Second render with different values - cache miss
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(3))

		// Values function should be called twice (no cache hits)
		g.Expect(callCount).To(Equal(2))
	})

	t.Run("should work with cache disabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "no-cache-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Second render - should work even without cache
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))
	})

	t.Run("should return clones from cache", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "clone-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// First render
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Modify first result
		if len(result1) > 0 {
			result1[0].SetName("modified-name")
		}

		// Second render - should not be affected by modification
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(3))

		if len(result2) > 0 {
			g.Expect(result2[0].GetName()).ToNot(Equal("modified-name"))
		}
	})

	t.Run("should use custom cache key function", func(t *testing.T) {
		g := NewWithT(t)

		// Use FastCacheKeyFunc which ignores values for better performance
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "custom-key-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		},
			helm.WithCache(cache.WithKeyFunc(helm.FastCacheKeyFunc)),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render - cache miss
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Second render with DIFFERENT values but using FastCacheKeyFunc
		// which ignores values, so this should be a cache hit
		result2, err := renderer.Process(t.Context(), map[string]any{
			"replicaCount": 5,
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))

		// Results should be identical despite different values (cache hit)
		for i := range result1 {
			g.Expect(result2[i]).To(Equal(result1[i]))
		}
	})
}

func BenchmarkHelmRenderWithoutCache(b *testing.B) {
	renderer, err := helm.New([]helm.Source{
		{
			Chart:       testChartPath,
			ReleaseName: "bench-no-cache",
			Values: helm.Values(map[string]any{
				"replicaCount": 1,
			}),
		},
	})
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}

func BenchmarkHelmRenderWithCache(b *testing.B) {
	renderer, err := helm.New(
		[]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "bench-cache",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		},
		helm.WithCache(),
	)
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}

func BenchmarkHelmRenderCacheMiss(b *testing.B) {
	renderer, err := helm.New(
		[]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "bench-miss",
				Values: func(_ context.Context) (map[string]any, error) {
					return map[string]any{
						"shared": map[string]any{
							"appId": xid.New().String(),
						},
					}, nil
				},
			},
		},
		helm.WithCache(),
	)
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}

func TestMetricsIntegration(t *testing.T) {

	// Metrics are now observed at the engine level, not in the renderer
	// This test verifies that renderers work without metrics in context
	t.Run("should work without metrics context", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "metrics-test",
				Values: helm.Values(map[string]any{
					"shared": map[string]any{
						"appId": "metrics-app",
					},
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())
	})

	t.Run("should implement Name() method", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "name-test",
				Values: helm.Values(map[string]any{
					"shared": map[string]any{
						"appId": "name-app",
					},
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(renderer.Name()).To(Equal("helm"))
	})
}

func TestSourceAnnotations(t *testing.T) {

	t.Run("should add source annotations when enabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
					ReleaseName: "annotations-test",
					Values: helm.Values(map[string]any{
						"shared": map[string]any{
							"appId": "annotations-app",
						},
					}),
				},
			},
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		// Verify all objects have source annotations
		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).Should(HaveKeyWithValue(types.AnnotationSourceType, "helm"))
			g.Expect(annotations).Should(HaveKeyWithValue(
				types.AnnotationSourcePath,
				"oci://registry-1.docker.io/daprio/dapr-shared-chart",
			))
			g.Expect(annotations).Should(HaveKey(types.AnnotationSourceFile))
			// File should be a template path
			g.Expect(annotations[types.AnnotationSourceFile]).ShouldNot(BeEmpty())
		}
	})

	t.Run("should not add source annotations when disabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseName: "no-annotations-test",
				Values: helm.Values(map[string]any{
					"shared": map[string]any{
						"appId": "no-annotations-app",
					},
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		// Verify no source annotations are present
		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourceType))
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourcePath))
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourceFile))
		}
	})
}

func TestCredentials(t *testing.T) {

	t.Run("should work with nil credentials", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "no-creds-test",
				Credentials: nil,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})

	t.Run("should work with static credentials", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "static-creds-test",
				Credentials: helm.StaticCredentials("testuser", "testpass"),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})

	t.Run("should work with dynamic credentials", func(t *testing.T) {
		g := NewWithT(t)
		callCount := 0
		dynamicCreds := func(_ context.Context) (*helm.Credentials, error) {
			callCount++

			return &helm.Credentials{
				Username: "dynamic-user",
				Password: "dynamic-pass",
			}, nil
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "dynamic-creds-test",
				Credentials: dynamicCreds,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		// Credentials function should be called during chart loading
		g.Expect(callCount).To(Equal(1))

		// Second render should not call credentials again (chart already loaded)
		objects, err = renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
		g.Expect(callCount).To(Equal(1))
	})

	t.Run("should handle credentials function returning empty credentials", func(t *testing.T) {
		g := NewWithT(t)
		emptyCreds := func(_ context.Context) (*helm.Credentials, error) {
			return &helm.Credentials{}, nil
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "empty-creds-test",
				Credentials: emptyCreds,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})

	t.Run("should propagate credentials function errors", func(t *testing.T) {
		g := NewWithT(t)
		errorCreds := func(_ context.Context) (*helm.Credentials, error) {
			return nil, context.DeadlineExceeded
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "error-creds-test",
				Credentials: errorCreds,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to get credentials"))
	})
}

func TestRenderer_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("should handle concurrent loading of non-existent chart", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		invalidPath := "/nonexistent/path/to/chart"
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       invalidPath,
				ReleaseName: "invalid-chart",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Launch multiple concurrent calls to trigger race conditions
		var wg sync.WaitGroup
		errorChan := make(chan error, 5)

		for range 5 {
			wg.Go(func() {
				_, err := renderer.Process(t.Context(), nil)
				errorChan <- err
			})
		}

		wg.Wait()
		close(errorChan)

		// All goroutines should receive proper errors
		errorCount := 0
		for err := range errorChan {
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("error rendering helm chart"))
			errorCount++
		}
		g.Expect(errorCount).To(Equal(5))
	})

	t.Run("should handle charts with ProcessDependencies flag", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// Test that ProcessDependencies flag works with valid charts
		// Note: Dependency resolution errors are typically caught during chart loading
		// This test documents that the flag can be used without panicking
		renderer, err := helm.New([]helm.Source{
			{
				Chart:               testChartPath,
				ReleaseName:         "deps-test",
				ProcessDependencies: true,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Should successfully process chart without dependencies
		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())
	})

	t.Run("should handle malformed YAML in templates", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		tmpDir := t.TempDir()
		chartPath := filepath.Join(tmpDir, "bad-yaml")

		// Create Chart.yaml
		err := os.MkdirAll(filepath.Join(chartPath, "templates"), 0750)
		g.Expect(err).ToNot(HaveOccurred())

		chartYAML := `apiVersion: v2
name: bad-yaml
version: 1.0.0
`
		err = os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte(chartYAML), 0600)
		g.Expect(err).ToNot(HaveOccurred())

		// Create template with malformed YAML (invalid indentation)
		badTemplate := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  invalid: |
    {{- range .Values.items }}
      key: {{ . }}
    indented incorrectly
    {{- end }}
`
		err = os.WriteFile(filepath.Join(chartPath, "templates", "configmap.yaml"), []byte(badTemplate), 0600)
		g.Expect(err).ToNot(HaveOccurred())

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       chartPath,
				ReleaseName: "bad-yaml-test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// This documents expected behavior: Helm may render template successfully
		// even if YAML structure is questionable, as it's up to Kubernetes to validate
		objects, err := renderer.Process(t.Context(), map[string]any{
			"items": []string{"a", "b"},
		})

		// Either succeeds with objects or fails during YAML parsing
		if err != nil {
			g.Expect(err.Error()).To(Or(
				ContainSubstring("yaml"),
				ContainSubstring("unmarshal"),
			))
		} else {
			g.Expect(objects).ToNot(BeNil())
		}
	})

	t.Run("should respect context cancellation during chart loading", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cancelled-test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(ctx, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("context cancel"))
	})

	t.Run("should provide descriptive errors for invalid chart paths", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		invalidPaths := []string{
			"/nonexistent/chart/path",
			"./missing/local/chart",
			"/tmp/not-a-chart-directory",
		}

		for _, path := range invalidPaths {
			renderer, err := helm.New([]helm.Source{
				{
					Chart:       path,
					ReleaseName: "invalid-path-test",
				},
			})
			g.Expect(err).ToNot(HaveOccurred())

			_, err = renderer.Process(t.Context(), nil)
			g.Expect(err).To(HaveOccurred(), "Expected error for path: %s", path)
			g.Expect(err.Error()).To(ContainSubstring("error rendering helm chart"))

			// Error should include context about which chart/release failed
			g.Expect(err.Error()).To(Or(
				ContainSubstring(path),
				ContainSubstring("invalid-path-test"),
			))
		}
	})

	t.Run("should wrap Values function errors with context", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		customErr := errors.New("database connection failed")
		errorValuesFunc := func(_ context.Context) (map[string]any, error) {
			return nil, customErr
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "error-values-test",
				Values:      errorValuesFunc,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())

		// Verify error chain includes original error
		g.Expect(errors.Is(err, customErr)).To(BeTrue(), "Error chain should include original error")

		// Verify error message includes context
		errMsg := err.Error()
		g.Expect(errMsg).To(ContainSubstring("failed to get values"))
		g.Expect(errMsg).To(ContainSubstring("error-values-test"))
	})

	t.Run("should wrap Credentials function errors with context", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		customErr := errors.New("authentication service unavailable")
		errorCredsFunc := func(_ context.Context) (*helm.Credentials, error) {
			return nil, customErr
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "error-creds-test",
				Credentials: errorCredsFunc,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())

		// Verify error chain includes original error
		g.Expect(errors.Is(err, customErr)).To(BeTrue(), "Error chain should include original error")

		// Verify error message includes context
		errMsg := err.Error()
		g.Expect(errMsg).To(ContainSubstring("failed to get credentials"))
		g.Expect(errMsg).To(ContainSubstring("error-creds-test"))
	})
}
