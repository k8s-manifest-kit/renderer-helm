package helm_test

import (
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/k8s"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// annotatedChartName is the chart name of the in-memory fixture whose
// template carries its own annotations.
const annotatedChartName = "annotated-app"

// annotatedChartTmpl renders a ConfigMap that ships its own annotations,
// including stale kit-managed keys, to characterize how renderer annotation
// phases interact with pre-existing metadata.
const annotatedChartTmpl = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-annotated
  annotations:
    existing: annotation
    manifests.k8s-manifests-kit/content.hash: sha256:stale
    manifests.k8s-manifests-kit/render.origin: stale
data:
  key: value
`

// annotatedChartFS returns an in-memory chart whose single template emits an
// object with pre-existing annotations.
func annotatedChartFS() fstest.MapFS {
	return fstest.MapFS{
		"chart/Chart.yaml": &fstest.MapFile{
			Data: []byte("apiVersion: v2\nname: " + annotatedChartName + "\nversion: 1.0.0\n"),
			Mode: 0o644,
		},
		"chart/values.yaml": &fstest.MapFile{
			Data: []byte("replicaCount: 1\n"),
			Mode: 0o644,
		},
		"chart/templates/annotated.yaml": &fstest.MapFile{
			Data: []byte(annotatedChartTmpl),
			Mode: 0o644,
		},
	}
}

// renderSimpleChart renders the checked-in simple-app chart with the given
// renderer options and returns the bare result.
func renderSimpleChart(t *testing.T, opts ...helm.RendererOption) []unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)

	renderer, err := helm.New([]helm.Source{
		{
			Chart:       testChartPath,
			ReleaseName: "annotation-app",
			Values:      helm.Values(map[string]any{"replicaCount": 1}),
		},
	}, opts...)
	g.Expect(err).ToNot(HaveOccurred())

	objects, err := renderer.Process(t.Context(), nil)
	g.Expect(err).ToNot(HaveOccurred())

	return objects
}

// renderAnnotatedChart renders the in-memory annotated-annotation chart with
// the given renderer options and returns the bare result.
func renderAnnotatedChart(t *testing.T, opts ...helm.RendererOption) []unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)

	renderer, err := helm.New([]helm.Source{
		{
			FS:          annotatedChartFS(),
			Chart:       "chart",
			ReleaseName: "annotation-app",
		},
	}, opts...)
	g.Expect(err).ToNot(HaveOccurred())

	objects, err := renderer.Process(t.Context(), nil)
	g.Expect(err).ToNot(HaveOccurred())

	return objects
}

// TestAnnotationApplication characterizes how source annotations, content
// hashes, and render origins are applied to CRDs and rendered templates.
func TestAnnotationApplication(t *testing.T) {

	t.Run("exact annotation map for CRDs and templates with hash over source metadata before origin", func(t *testing.T) {
		g := NewWithT(t)

		// CRDs are processed before templates; template files follow sorted
		// key order.
		expectedFiles := []string{
			"crds/application-crd.yaml",
			"simple-app/templates/deployment.yaml",
			"simple-app/templates/service.yaml",
		}
		expectedKinds := []string{"CustomResourceDefinition", "Deployment", "Service"}

		bare := renderSimpleChart(t, helm.WithContentHash(false))
		annotated := renderSimpleChart(t, helm.WithSourceAnnotations(true))

		g.Expect(bare).To(HaveLen(3))
		g.Expect(annotated).To(HaveLen(3))

		for i := range annotated {
			g.Expect(annotated[i].GetKind()).To(Equal(expectedKinds[i]))

			expected := bare[i].DeepCopy()
			expected.SetAnnotations(map[string]string{
				types.AnnotationSourceType: "helm",
				types.AnnotationSourcePath: testChartPath,
				types.AnnotationSourceFile: expectedFiles[i],
			})

			g.Expect(annotated[i].GetAnnotations()).To(Equal(map[string]string{
				types.AnnotationSourceType:   "helm",
				types.AnnotationSourcePath:   testChartPath,
				types.AnnotationSourceFile:   expectedFiles[i],
				types.AnnotationContentHash:  k8s.ContentHash(expected),
				types.AnnotationRenderOrigin: types.RenderOriginLive,
			}))
		}
	})

	t.Run("pre-existing template annotations are preserved and included in the hash input", func(t *testing.T) {
		g := NewWithT(t)

		bare := renderAnnotatedChart(t, helm.WithContentHash(false))
		annotated := renderAnnotatedChart(t, helm.WithSourceAnnotations(true))

		g.Expect(bare).To(HaveLen(1))
		g.Expect(annotated).To(HaveLen(1))

		expected := bare[0].DeepCopy()
		expected.SetAnnotations(map[string]string{
			"existing":                   "annotation",
			types.AnnotationContentHash:  "sha256:stale",
			types.AnnotationRenderOrigin: "stale",
			types.AnnotationSourceType:   "helm",
			types.AnnotationSourcePath:   "chart",
			types.AnnotationSourceFile:   annotatedChartName + "/templates/annotated.yaml",
		})

		annotations := annotated[0].GetAnnotations()
		g.Expect(annotations).To(HaveKeyWithValue("existing", "annotation"))
		g.Expect(annotations).To(HaveKeyWithValue(
			types.AnnotationContentHash,
			k8s.ContentHash(expected),
		))
		g.Expect(annotations).NotTo(HaveKeyWithValue(types.AnnotationContentHash, "sha256:stale"))
		g.Expect(annotations).To(HaveKeyWithValue(types.AnnotationRenderOrigin, types.RenderOriginLive))
	})

	t.Run("source annotations without content hash leave no hash key", func(t *testing.T) {
		g := NewWithT(t)

		annotated := renderSimpleChart(t,
			helm.WithSourceAnnotations(true),
			helm.WithContentHash(false),
		)

		for i := range annotated {
			annotations := annotated[i].GetAnnotations()
			g.Expect(annotations).To(HaveKeyWithValue(types.AnnotationSourceType, "helm"))
			g.Expect(annotations).To(HaveKeyWithValue(types.AnnotationSourcePath, testChartPath))
			g.Expect(annotations).To(HaveKey(types.AnnotationSourceFile))
			g.Expect(annotations).To(HaveKeyWithValue(types.AnnotationRenderOrigin, types.RenderOriginLive))
			g.Expect(annotations).NotTo(HaveKey(types.AnnotationContentHash))
		}
	})

	t.Run("disabled source annotations inject no origin key", func(t *testing.T) {
		g := NewWithT(t)

		// Content hashes default to enabled.
		defaults := renderSimpleChart(t)

		for i := range defaults {
			annotations := defaults[i].GetAnnotations()
			g.Expect(annotations).To(HaveKey(types.AnnotationContentHash))
			g.Expect(annotations).NotTo(HaveKey(types.AnnotationSourceType))
			g.Expect(annotations).NotTo(HaveKey(types.AnnotationSourcePath))
			g.Expect(annotations).NotTo(HaveKey(types.AnnotationSourceFile))
			g.Expect(annotations).NotTo(HaveKey(types.AnnotationRenderOrigin))
		}
	})
}

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
