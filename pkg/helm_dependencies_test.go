package helm_test

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/maps"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// TestRenderer_DependencyToggle covers repeated renders on one renderer whose
// render-time values enable and disable the same vendored subchart. Helm
// dependency processing removes a disabled subchart from the chart graph and
// from its metadata, so the renderer must keep the first-loaded chart
// pristine and give every render its own mutable copy.
func TestRenderer_DependencyToggle(t *testing.T) {
	t.Parallel()

	const releaseName = "deps-toggle"

	t.Run("should disable, enable, then disable a subchart again", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		renderer, err := helm.New([]helm.Source{{
			Chart:               withSubchartsPath,
			ReleaseName:         releaseName,
			ProcessDependencies: true,
		}})
		g.Expect(err).ToNot(HaveOccurred())

		childAOff := dependencyExpectation{
			childAEnabled: false,
			childBEnabled: true,
			childCEnabled: true,
			greeting:      parentDefaultGreeting,
		}

		// Enabled by default: the subchart resources and CRD are rendered.
		enabled, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, enabled, releaseName, allSubchartsEnabled())

		// Disabled through render-time values.
		disabled, err := renderer.Process(t.Context(), disableValues("child-a"))
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, disabled, releaseName, childAOff)

		// Re-enabled on the same renderer: the first-loaded chart must still
		// describe the subchart.
		reEnabled, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, reEnabled, releaseName, allSubchartsEnabled())

		// Disabled again.
		disabledAgain, err := renderer.Process(t.Context(), disableValues("child-a"))
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, disabledAgain, releaseName, childAOff)
	})

	t.Run("should disable and re-enable different subcharts between renders", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		renderer, err := helm.New([]helm.Source{{
			Chart:               withSubchartsPath,
			ReleaseName:         releaseName,
			ProcessDependencies: true,
		}})
		g.Expect(err).ToNot(HaveOccurred())

		childBOff := dependencyExpectation{
			childAEnabled: true,
			childBEnabled: false,
			childCEnabled: true,
			greeting:      parentDefaultGreeting,
		}
		childCOff := dependencyExpectation{
			childAEnabled: true,
			childBEnabled: true,
			childCEnabled: false,
			greeting:      parentDefaultGreeting,
		}

		objects, err := renderer.Process(t.Context(), disableValues("child-b"))
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, objects, releaseName, childBOff)

		// Disabling another subchart must not lose the previously disabled one.
		objects, err = renderer.Process(t.Context(), disableValues("child-c-alias"))
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, objects, releaseName, childCOff)

		objects, err = renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertDependencyRender(g, objects, releaseName, allSubchartsEnabled())
	})
}

// TestRenderer_ValuesPrecedence asserts the chart-default < source <
// render-time value precedence with direct field checks against the rendered
// ConfigMaps, including nested-map merging and immutability of the caller's
// input maps.
func TestRenderer_ValuesPrecedence(t *testing.T) {
	t.Parallel()

	newRenderer := func(t *testing.T, g *WithT, sourceValues map[string]any) *helm.Renderer {
		t.Helper()

		renderer, err := helm.New([]helm.Source{{
			Chart:               withSubchartsPath,
			ReleaseName:         "deps-precedence",
			ProcessDependencies: true,
			Values:              helm.Values(sourceValues),
		}})
		g.Expect(err).ToNot(HaveOccurred())

		return renderer
	}

	parentData := func(t *testing.T, g *WithT, objects []unstructured.Unstructured) map[string]any {
		t.Helper()

		parent := findConfigMap(objects, "deps-precedence-parent-config")
		g.Expect(parent).ToNot(BeNil())

		return configMapData(g, parent)
	}

	childData := func(t *testing.T, g *WithT, objects []unstructured.Unstructured, name string) map[string]any {
		t.Helper()

		child := findConfigMap(objects, "deps-precedence-"+name+"-config")
		g.Expect(child).ToNot(BeNil())

		return configMapData(g, child)
	}

	t.Run("should apply chart defaults without overrides", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		objects, err := newRenderer(t, g, nil).Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		data := parentData(t, g, objects)
		g.Expect(data).To(HaveKeyWithValue("greeting", parentDefaultGreeting))
		g.Expect(data).To(HaveKeyWithValue("overridden", "parent-default-overridden"))
		g.Expect(data).To(HaveKeyWithValue("nested-key", "parent-default-nested-key"))
		g.Expect(data).To(HaveKeyWithValue("nested-extra", "parent-default-nested-extra"))
		// import-values from child-a are merged into the parent defaults
		// during dependency processing.
		g.Expect(data).To(HaveKeyWithValue("imported-data", childAExportData))

		childA := childData(t, g, objects, "child-a")
		g.Expect(childA).To(HaveKeyWithValue("message", childADefaultMessage))
		g.Expect(childA).To(HaveKeyWithValue("keep", childADefaultKeep))
	})

	t.Run("should let source values override chart defaults with nested merging", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		sourceValues := map[string]any{
			"greeting": "source-greeting",
			"nested": map[string]any{
				"key": "source-nested-key",
			},
			"child-a": map[string]any{
				"config": map[string]any{
					"message": "source-child-a-message",
				},
			},
		}

		objects, err := newRenderer(t, g, sourceValues).Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		data := parentData(t, g, objects)
		g.Expect(data).To(HaveKeyWithValue("greeting", "source-greeting"))
		// Nested maps merge: the untouched sibling key keeps its default.
		g.Expect(data).To(HaveKeyWithValue("nested-key", "source-nested-key"))
		g.Expect(data).To(HaveKeyWithValue("nested-extra", "parent-default-nested-extra"))
		g.Expect(data).To(HaveKeyWithValue("overridden", "parent-default-overridden"))

		// Subchart values merge the same way.
		childA := childData(t, g, objects, "child-a")
		g.Expect(childA).To(HaveKeyWithValue("message", "source-child-a-message"))
		g.Expect(childA).To(HaveKeyWithValue("keep", childADefaultKeep))
	})

	t.Run("should let render-time values override source values and chart defaults", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		sourceValues := map[string]any{
			"greeting": "source-greeting",
			"nested": map[string]any{
				"key": "source-nested-key",
			},
		}
		renderTimeValues := map[string]any{
			"greeting": "render-time-greeting",
			"nested": map[string]any{
				"key": "render-time-nested-key",
			},
			"imported": map[string]any{
				"data": "render-time-imported",
			},
		}

		objects, err := newRenderer(t, g, sourceValues).Process(t.Context(), renderTimeValues)
		g.Expect(err).ToNot(HaveOccurred())

		data := parentData(t, g, objects)
		// chart-default < source < render-time.
		g.Expect(data).To(HaveKeyWithValue("greeting", "render-time-greeting"))
		g.Expect(data).To(HaveKeyWithValue("nested-key", "render-time-nested-key"))
		// Keys only set at a lower precedence level keep their values.
		g.Expect(data).To(HaveKeyWithValue("nested-extra", "parent-default-nested-extra"))
		// User values also override values imported from subcharts.
		g.Expect(data).To(HaveKeyWithValue("imported-data", "render-time-imported"))
	})

	t.Run("should not mutate source and render-time input maps", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		sourceValues := map[string]any{
			"greeting": "source-greeting",
			"nested": map[string]any{
				"key": "source-nested-key",
			},
			"child-a": map[string]any{
				"config": map[string]any{
					"message": "source-child-a-message",
				},
			},
		}
		renderTimeValues := map[string]any{
			"greeting": "render-time-greeting",
		}
		sourceSnapshot := maps.DeepCloneMap(sourceValues)
		renderTimeSnapshot := maps.DeepCloneMap(renderTimeValues)

		for range 3 {
			_, err := newRenderer(t, g, sourceValues).Process(t.Context(), renderTimeValues)
			g.Expect(err).ToNot(HaveOccurred())
		}

		g.Expect(sourceValues).To(Equal(sourceSnapshot))
		g.Expect(renderTimeValues).To(Equal(renderTimeSnapshot))
	})
}

// TestRenderer_DependencyAlias covers a dependency referenced through an
// alias: the subchart renders once, under the alias values namespace, while
// keeping its template identity.
func TestRenderer_DependencyAlias(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	renderer, err := helm.New([]helm.Source{{
		Chart:               withSubchartsPath,
		ReleaseName:         "deps-alias",
		ProcessDependencies: true,
	}})
	g.Expect(err).ToNot(HaveOccurred())

	objects, err := renderer.Process(t.Context(), map[string]any{
		"child-c-alias": map[string]any{
			"config": map[string]any{
				"message": "aliased-message",
			},
		},
	})
	g.Expect(err).ToNot(HaveOccurred())
	assertDependencyRender(g, objects, "deps-alias", allSubchartsEnabled())

	// Exactly one child-c ConfigMap exists (ConsistOf above guards the count)
	// and its values come from the alias namespace merged over the subchart
	// defaults.
	child := findConfigMap(objects, "deps-alias-child-c-config")
	g.Expect(child).ToNot(BeNil())

	data := configMapData(g, child)
	g.Expect(data).To(HaveKeyWithValue("message", "aliased-message"))
	g.Expect(data).To(HaveKeyWithValue("keep", childCDefaultKeep))
}

// TestRenderer_DependencyEmissionOrder covers a chart with multiple
// CRD-bearing subcharts: repeated cache-disabled renders must be identical,
// with dependency order (and therefore CRD/resource emission order) preserved
// from the first-loaded chart rather than rebuilt from map iteration.
func TestRenderer_DependencyEmissionOrder(t *testing.T) {
	t.Parallel()

	for _, processDeps := range []bool{true, false} {
		t.Run(fmt.Sprintf("process dependencies %t", processDeps), func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			renderer, err := helm.New([]helm.Source{{
				Chart:               withSubchartsPath,
				ReleaseName:         "deps-order",
				ProcessDependencies: processDeps,
			}})
			g.Expect(err).ToNot(HaveOccurred())

			first, err := renderer.Process(t.Context(), nil)
			g.Expect(err).ToNot(HaveOccurred())
			// Parent ConfigMap, three subchart ConfigMaps, three subchart CRDs.
			g.Expect(first).To(HaveLen(7))

			// Without dependency processing the imported values are absent;
			// with it, child-a's exports are merged into the parent defaults.
			parent := findConfigMap(first, "deps-order-parent-config")
			g.Expect(parent).ToNot(BeNil())

			expectedImported := childAExportData
			if !processDeps {
				expectedImported = notImportedFallbackData
			}
			g.Expect(configMapData(g, parent)).To(HaveKeyWithValue("imported-data", expectedImported))

			for range 5 {
				next, err := renderer.Process(t.Context(), nil)
				g.Expect(err).ToNot(HaveOccurred())
				// Order-sensitive comparison: identical objects in identical
				// order on every render.
				g.Expect(next).To(Equal(first))
			}
		})
	}
}

// TestRenderer_DependencyProcessingWithCache covers dependency processing
// with the output cache enabled: cache hits must not depend on, or cause,
// chart mutation, and cached results must stay independent of mutations
// applied to returned objects.
func TestRenderer_DependencyProcessingWithCache(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	renderer, err := helm.New([]helm.Source{{
		Chart:               withSubchartsPath,
		ReleaseName:         "deps-cache",
		ProcessDependencies: true,
	}}, helm.WithCache(), helm.WithSourceAnnotations(true))
	g.Expect(err).ToNot(HaveOccurred())

	// First render: live, all subcharts enabled.
	live, err := renderer.Process(t.Context(), nil)
	g.Expect(err).ToNot(HaveOccurred())
	assertDependencyRender(g, live, "deps-cache", allSubchartsEnabled())
	assertRenderOrigin(g, live, types.RenderOriginLive)

	// Second render with identical values: cache hit.
	hit, err := renderer.Process(t.Context(), nil)
	g.Expect(err).ToNot(HaveOccurred())
	assertDependencyRender(g, hit, "deps-cache", allSubchartsEnabled())
	assertRenderOrigin(g, hit, types.RenderOriginCache)

	// Mutating a returned object must not affect later renders.
	mutated := findConfigMap(hit, "deps-cache-parent-config")
	g.Expect(mutated).ToNot(BeNil())
	g.Expect(unstructured.SetNestedField(mutated.Object, "mutated", "data", "greeting")).To(Succeed())

	again, err := renderer.Process(t.Context(), nil)
	g.Expect(err).ToNot(HaveOccurred())
	assertDependencyRender(g, again, "deps-cache", allSubchartsEnabled())
	assertRenderOrigin(g, again, types.RenderOriginCache)

	// Different values: cache miss and a live render with the subchart
	// disabled.
	disabled, err := renderer.Process(t.Context(), disableValues("child-a"))
	g.Expect(err).ToNot(HaveOccurred())
	assertRenderOrigin(g, disabled, types.RenderOriginLive)
	assertDependencyRender(g, disabled, "deps-cache", dependencyExpectation{
		childAEnabled: false,
		childBEnabled: true,
		childCEnabled: true,
		greeting:      parentDefaultGreeting,
	})

	// After all the hits and misses above, a fresh live render must still see
	// the pristine chart: the subchart comes back.
	reEnabled, err := renderer.Process(t.Context(), map[string]any{
		"greeting": "post-cache-greeting",
	})
	g.Expect(err).ToNot(HaveOccurred())
	assertRenderOrigin(g, reEnabled, types.RenderOriginLive)
	assertDependencyRender(g, reEnabled, "deps-cache", dependencyExpectation{
		childAEnabled: true,
		childBEnabled: true,
		childCEnabled: true,
		greeting:      "post-cache-greeting",
	})
}

// TestRenderer_ConcurrentDependencyRenders renders the same renderer
// concurrently with different dependency and value settings. The cold round
// races the lazy chart load; the warm round then reuses the loaded chart.
// Every worker must observe the output its own settings produce.
func TestRenderer_ConcurrentDependencyRenders(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	renderer, err := helm.New([]helm.Source{{
		Chart:               withSubchartsPath,
		ReleaseName:         "deps-concurrent",
		ProcessDependencies: true,
	}})
	g.Expect(err).ToNot(HaveOccurred())

	runConcurrentDependencyRound(t, g, renderer, "cold", "cold")
	// The chart is loaded at this point: the warm round exercises concurrent
	// reuse of the source snapshot rather than its lazy load.
	runConcurrentDependencyRound(t, g, renderer, "warm", "warm")
}
