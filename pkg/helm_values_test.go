package helm_test

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	engine "github.com/k8s-manifest-kit/engine/pkg"
	"github.com/k8s-manifest-kit/engine/pkg/types"
	utilcache "github.com/k8s-manifest-kit/pkg/util/cache"
	utilmaps "github.com/k8s-manifest-kit/pkg/util/maps"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// TestProcessNilReceiver verifies that a typed-nil Renderer answers the
// sentinel instead of panicking, both when Process is called directly and
// when the renderer runs behind the engine, whose validation accepts the
// typed nil because Name is nil-safe.
func TestProcessNilReceiver(t *testing.T) {
	t.Parallel()

	t.Run("should return the sentinel from a direct call", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		var renderer *helm.Renderer

		objects, err := renderer.Process(t.Context(), nil)

		g.Expect(err).To(MatchError(types.ErrRendererNil))
		g.Expect(objects).To(BeNil())
	})

	t.Run("should return the sentinel through the engine", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		var renderer *helm.Renderer

		e, err := engine.New(engine.WithRenderer(renderer))
		g.Expect(err).ToNot(HaveOccurred(), "engine construction accepts the typed-nil built-in")

		objects, err := e.Render(t.Context())

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, types.ErrRendererNil)).To(BeTrue())
		g.Expect(objects).To(BeNil())
	})
}

// TestProcessRejectsCyclicRenderTimeValues verifies that direct Process
// calls reject supported map and slice cycles in render-time values before
// the per-source deep clone, naming the chart and release, and that the
// renderer stays usable afterwards.
func TestProcessRejectsCyclicRenderTimeValues(t *testing.T) {
	t.Parallel()

	newRenderer := func(t *testing.T) *helm.Renderer {
		t.Helper()

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cyclic-values-test",
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		return renderer
	}

	t.Run("should reject a self-referential map value", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := map[string]any{}
		cyclic["self"] = cyclic

		renderer := newRenderer(t)

		objects, err := renderer.Process(t.Context(), types.Values{"cyclic": cyclic})

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, utilmaps.ErrCycle)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("cyclic render-time values"))
		g.Expect(err.Error()).To(ContainSubstring("simple-app"))
		g.Expect(err.Error()).To(ContainSubstring("cyclic-values-test"))
		g.Expect(objects).To(BeNil())
	})

	t.Run("should reject a self-referential slice value", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := make([]any, 1)
		cyclic[0] = cyclic

		renderer := newRenderer(t)

		objects, err := renderer.Process(t.Context(), types.Values{"cyclic": cyclic})

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, utilmaps.ErrCycle)).To(BeTrue())
		g.Expect(objects).To(BeNil())
	})

	t.Run("should keep the renderer usable after a rejection", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := map[string]any{}
		cyclic["self"] = cyclic

		renderer := newRenderer(t)

		_, err := renderer.Process(t.Context(), types.Values{"cyclic": cyclic})
		g.Expect(err).To(HaveOccurred())

		objects, err := renderer.Process(t.Context(), types.Values{"replicaCount": 2})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())
	})
}

// TestValuesCallbackRejectsCyclicSourceValues verifies that resolved source
// values are validated before the merge on cold and warm output-cache paths,
// that an overlay replacing the cyclic branch does not rescue the input, and
// that the error keeps the source context and the shared sentinel.
func TestValuesCallbackRejectsCyclicSourceValues(t *testing.T) {
	t.Parallel()

	t.Run("should reject a cyclic callback result on a cold cache", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := map[string]any{}
		cyclic["self"] = cyclic

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cyclic-callback-test",
				Values: func(_ context.Context) (types.Values, error) {
					return types.Values{"cyclic": cyclic}, nil
				},
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, utilmaps.ErrCycle)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("cyclic source values"))
		g.Expect(err.Error()).To(ContainSubstring("simple-app"))
		g.Expect(err.Error()).To(ContainSubstring("cyclic-callback-test"))
		g.Expect(objects).To(BeNil())
	})

	t.Run("should reject a cyclic callback result on a warm cache", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		current := types.Values{"replicaCount": 2}
		callbackInvocations := 0

		// The constant cache key keeps the earlier successful entry usable
		// for any later render of this source, so the rejection below
		// cannot be explained by a key change: values resolve and validate
		// before the cache is consulted.
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "warm-cache-cyclic-test",
				Values: func(_ context.Context) (types.Values, error) {
					callbackInvocations++

					return current, nil
				},
			},
		},
			helm.WithCache(utilcache.WithKeyFunc(constantCacheKey)),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render populates the output cache, second render proves it
		// is warm, and both resolve the callback.
		live, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertRenderOrigin(g, live, types.RenderOriginLive)

		cached, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertRenderOrigin(g, cached, types.RenderOriginCache)
		g.Expect(callbackInvocations).To(Equal(2), "values resolve on warm-cache hits")

		// The callback now returns a cyclic tree: the usable cached entry
		// must not bypass validation, and the rejection happens before the
		// merge and before the cache could serve the entry.
		cyclic := map[string]any{}
		cyclic["self"] = cyclic
		current = types.Values{"cyclic": cyclic}

		objects, err := renderer.Process(t.Context(), nil)

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, utilmaps.ErrCycle)).To(BeTrue())
		g.Expect(callbackInvocations).To(Equal(3))
		g.Expect(objects).To(BeNil())
	})

	t.Run("should reject a cyclic branch that the overlay replaces", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := map[string]any{}
		cyclic["self"] = cyclic

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "overlaid-cyclic-test",
				Values: func(_ context.Context) (types.Values, error) {
					return types.Values{"extra": cyclic}, nil
				},
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// The render-time overlay would replace the cyclic branch entirely;
		// the source tree is still rejected whole before the merge runs.
		objects, err := renderer.Process(t.Context(), types.Values{"extra": map[string]any{"safe": true}})

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, utilmaps.ErrCycle)).To(BeTrue())
		g.Expect(objects).To(BeNil())
	})
}

// TestDeselectedSourceSkipsValueValidation verifies that a source rejected
// by its selector neither invokes its values callback nor validates the
// render-time values: per-source value boundaries apply to selected sources
// only, and the engine's global rejection is a separate boundary.
func TestDeselectedSourceSkipsValueValidation(t *testing.T) {
	t.Parallel()

	t.Run("should not validate values or invoke callbacks for deselected sources", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cyclic := map[string]any{}
		cyclic["self"] = cyclic

		callbackInvocations := 0

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "deselected-test",
				Values: func(_ context.Context) (types.Values, error) {
					callbackInvocations++

					return types.Values{"cyclic": cyclic}, nil
				},
			},
		},
			helm.WithSourceSelector(func(_ context.Context, _ helm.Source) (bool, error) {
				return false, nil
			}),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), types.Values{"cyclic": cyclic})

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(BeEmpty())
		g.Expect(callbackInvocations).To(BeZero(), "deselected sources must not invoke their callbacks")
	})
}

// TestValuesSharedReferencesRender verifies that shared references and a
// slice holding a prefix of itself — aliasing without cycles — render
// correctly when the chart consumes only safe scalar leaves.
func TestValuesSharedReferencesRender(t *testing.T) {
	t.Parallel()

	t.Run("should render shared references and overlapping prefixes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		shared := map[string]any{"repository": "nginx", "tag": "1.14.2"}

		items := make([]any, 2)
		items[0] = shared
		items[1] = items[:1]

		values := types.Values{
			"replicaCount": 3,
			"image":        shared,
			"reused":       shared,
			"items":        items,
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "shared-refs-test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), values)

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())
		g.Expect(deploymentReplicas(g, objects)).To(Equal(int64(3)))
	})
}

// TestSourcePostRendererChainIndex verifies that a failing source-specific
// post-renderer reports the adopted engine chain index while preserving the
// cause and the chart and release context.
func TestSourcePostRendererChainIndex(t *testing.T) {
	t.Parallel()

	t.Run("should report the chain index with chart context", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		errHook := errors.New("hook failed")

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "hook-index-test",
				PostRenderers: []types.PostRenderer{
					func(_ context.Context, _ []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
						return nil, errHook
					},
				},
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)

		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, errHook)).To(BeTrue(), "the hook cause stays discoverable")
		g.Expect(err.Error()).To(ContainSubstring("post-renderer error at chain index 0"))
		g.Expect(err.Error()).To(ContainSubstring("source post-renderer error"))
		g.Expect(err.Error()).To(ContainSubstring("hook-index-test"))
		g.Expect(objects).To(BeNil())
	})
}
