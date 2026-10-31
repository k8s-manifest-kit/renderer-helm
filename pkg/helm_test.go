package helm_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/rs/xid"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/k8s-manifest-kit/engine/pkg/filter/meta/gvk"
	"github.com/k8s-manifest-kit/engine/pkg/transformer/meta/labels"
	"github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

//nolint:cyclop,goconst // Test function complexity and string repetition acceptable for readability
func TestRenderer(t *testing.T) {

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

	t.Run("should render chart from io/fs filesystem", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				FS:          mapChartFS(t),
				Chart:       "chart",
				ReleaseName: "iofs-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 2,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		var foundCRD, foundDeployment, foundService bool
		for _, obj := range objects {
			switch obj.GetKind() {
			case "CustomResourceDefinition":
				foundCRD = true
			case "Deployment":
				foundDeployment = true
				g.Expect(obj.GetName()).To(Equal("iofs-test-deployment"))
			case "Service":
				foundService = true
			default:
				// Ignore other kinds.
			}
		}

		g.Expect(foundCRD).To(BeTrue(), "Should have rendered CRD")
		g.Expect(foundDeployment).To(BeTrue(), "Should have rendered Deployment")
		g.Expect(foundService).To(BeTrue(), "Should have rendered Service")
	})

	t.Run("should render with dynamic values", func(t *testing.T) {
		g := NewWithT(t)
		dynamicValues := func(_ context.Context) (types.Values, error) {
			return types.Values{
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

		valuesFn := func(ctx context.Context) (types.Values, error) {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return types.Values{}, nil
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

	t.Run("should reject filesystem source with Repo", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				FS:          fstest.MapFS{},
				Repo:        "https://charts.example.com",
				Chart:       ".",
				ReleaseName: "test",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(helm.IsValidationError(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("cannot be combined with Repo or OCI references"))
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
				// Points at a local server so the lookup fails without
				// contacting an external registry.
				Chart:       newUnavailableRegistry(t),
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
		g.Expect(result).To(Equal(types.Values(staticValues)))
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
		g.Expect(result).To(Equal(types.Values(map[string]any{})))
	})

	t.Run("should handle Values function returning nil", func(t *testing.T) {
		g := NewWithT(t)

		nilValuesFunc := func(_ context.Context) (types.Values, error) {
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

func TestMetricsIntegration(t *testing.T) {

	// Metrics are now observed at the engine level, not in the renderer
	// This test verifies that renderers work without metrics in context
	t.Run("should work without metrics context", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "metrics-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
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
		dynamicCreds := func(_ context.Context) (*locator.Credentials, error) {
			callCount++

			return &locator.Credentials{
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

		// Credentials are resolved lazily and skipped for local paths.
		g.Expect(callCount).To(Equal(0))
	})

	t.Run("should handle credentials function returning empty credentials", func(t *testing.T) {
		g := NewWithT(t)
		emptyCreds := func(_ context.Context) (*locator.Credentials, error) {
			return &locator.Credentials{}, nil
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

	t.Run("should not call credentials for local chart paths", func(t *testing.T) {
		g := NewWithT(t)
		errorCreds := func(_ context.Context) (*locator.Credentials, error) {
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

		// Credentials are not resolved for local chart paths, so this succeeds
		// even though the credentials function would return an error.
		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})
}
