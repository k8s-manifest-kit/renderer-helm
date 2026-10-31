package helm_test

import (
	"testing"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

func TestReleaseNamespace(t *testing.T) {

	t.Run("should set Release.Namespace from source", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:            testChartPath,
				ReleaseName:      "ns-test",
				ReleaseNamespace: "my-namespace",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		for _, obj := range objects {
			if obj.GetKind() == "Deployment" {
				g.Expect(obj.GetNamespace()).To(Equal("my-namespace"))
			}
		}
	})

	t.Run("should default Release.Namespace to empty when not set", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "no-ns-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		for _, obj := range objects {
			if obj.GetKind() == "Deployment" {
				g.Expect(obj.GetNamespace()).To(BeEmpty())
			}
		}
	})
}

func TestSourceAnnotations(t *testing.T) {

	t.Run("should add source annotations when enabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "annotations-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		// Verify all objects have source annotations
		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).Should(HaveKeyWithValue(types.AnnotationSourceType, "helm"))
			g.Expect(annotations).Should(HaveKeyWithValue(
				types.AnnotationRenderOrigin,
				types.RenderOriginLive,
			))
			g.Expect(annotations).Should(HaveKeyWithValue(
				types.AnnotationSourcePath,
				testChartPath,
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
				Chart:       testChartPath,
				ReleaseName: "no-annotations-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))

		// Verify no source annotations are present
		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourceType))
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourcePath))
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationSourceFile))
		}
	})
}

func TestContentHash(t *testing.T) {

	t.Run("should add content hash annotation by default", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "hash-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).Should(HaveKey(types.AnnotationContentHash))
			g.Expect(annotations[types.AnnotationContentHash]).Should(MatchRegexp("^sha256:[0-9a-f]{64}$"))
		}
	})

	t.Run("should not add content hash when disabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "no-hash-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
			helm.WithContentHash(false),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		for _, obj := range objects {
			annotations := obj.GetAnnotations()
			g.Expect(annotations).ShouldNot(HaveKey(types.AnnotationContentHash))
		}
	})

	t.Run("hash should change when values change", func(t *testing.T) {
		g := NewWithT(t)

		r1, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "hash-change",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		objects1, err := r1.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects1).ToNot(BeEmpty())

		r2, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "hash-change",
				Values: helm.Values(map[string]any{
					"replicaCount": 5,
				}),
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		objects2, err := r2.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects2).ToNot(BeEmpty())

		// Find the Deployment in each result -- replicaCount is templated so hash must differ
		var hash1, hash2 string
		for _, obj := range objects1 {
			if obj.GetKind() == "Deployment" {
				hash1 = obj.GetAnnotations()[types.AnnotationContentHash]
			}
		}
		for _, obj := range objects2 {
			if obj.GetKind() == "Deployment" {
				hash2 = obj.GetAnnotations()[types.AnnotationContentHash]
			}
		}

		g.Expect(hash1).ShouldNot(BeEmpty())
		g.Expect(hash2).ShouldNot(BeEmpty())
		g.Expect(hash1).ShouldNot(Equal(hash2))
	})

	t.Run("cached results should preserve content hash", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "cache-hash-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 2,
					}),
				},
			},
			helm.WithCache(),
		)
		g.Expect(err).ToNot(HaveOccurred())

		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).ToNot(BeEmpty())

		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))

		for i := range result1 {
			hash1 := result1[i].GetAnnotations()[types.AnnotationContentHash]
			hash2 := result2[i].GetAnnotations()[types.AnnotationContentHash]
			g.Expect(hash1).ShouldNot(BeEmpty())
			g.Expect(hash1).Should(Equal(hash2))
		}
	})
}
