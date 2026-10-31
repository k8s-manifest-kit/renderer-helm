package helm_test

import (
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

const withSubchartsPath = "../config/test/charts/with-subcharts"

const importValuesProbePath = "../config/test/charts/import-values-probe"

// Values baked into the with-subcharts fixture.
const (
	parentDefaultGreeting   = "parent-default-greeting"
	childADefaultMessage    = "child-a-default-message"
	childAExportData        = "child-a-export-data"
	childADefaultKeep       = "child-a-default-keep"
	childCDefaultKeep       = "child-c-default-keep"
	notImportedFallbackData = "not-imported"
)

// dependencyExpectation describes the render output expected from the
// with-subcharts fixture for one combination of enabled subcharts and
// render-time greeting override.
type dependencyExpectation struct {
	childAEnabled bool
	childBEnabled bool
	childCEnabled bool
	greeting      string
}

// allSubchartsEnabled is the default render of the fixture: every declared
// dependency is enabled because no condition disables it.
func allSubchartsEnabled() dependencyExpectation {
	return dependencyExpectation{
		childAEnabled: true,
		childBEnabled: true,
		childCEnabled: true,
		greeting:      parentDefaultGreeting,
	}
}

// disableValues returns render-time values that disable the given subchart
// through its dependency condition.
func disableValues(subchart string) map[string]any {
	return map[string]any{
		subchart: map[string]any{"enabled": false},
	}
}

// assertDependencyRender checks the exact object identities produced by the
// with-subcharts fixture and the greeting rendered into the parent ConfigMap.
func assertDependencyRender(
	g *WithT,
	objects []unstructured.Unstructured,
	releaseName string,
	exp dependencyExpectation,
) {
	expected := []any{
		objectIdentity("ConfigMap", releaseName+"-parent-config"),
	}

	subcharts := []struct {
		enabled    bool
		configName string
		crdName    string
	}{
		{exp.childAEnabled, releaseName + "-child-a-config", "childas.example.com"},
		{exp.childBEnabled, releaseName + "-child-b-config", "childbs.example.com"},
		{exp.childCEnabled, releaseName + "-child-c-config", "childcs.example.com"},
	}

	for _, subchart := range subcharts {
		if subchart.enabled {
			expected = append(expected,
				objectIdentity("ConfigMap", subchart.configName),
				objectIdentity("CustomResourceDefinition", subchart.crdName),
			)
		}
	}

	g.Expect(objectKindNames(objects)).To(ConsistOf(expected...))

	parent := findConfigMap(objects, releaseName+"-parent-config")
	g.Expect(parent).ToNot(BeNil(), "parent ConfigMap should be rendered")
	g.Expect(configMapData(g, parent)).To(HaveKeyWithValue("greeting", exp.greeting))
}

// objectIdentity returns the kind/name identity of a rendered object.
func objectIdentity(kind string, name string) string {
	return kind + "/" + name
}

// objectKindNames returns the kind/name identities of the rendered objects
// in output order. It is deliberately distinct from objectIdentities in the
// integration suite, which uses full GVK and namespace/name identities.
func objectKindNames(objects []unstructured.Unstructured) []string {
	identities := make([]string, 0, len(objects))
	for i := range objects {
		identities = append(identities, objectIdentity(objects[i].GetKind(), objects[i].GetName()))
	}

	return identities
}

// findConfigMap returns the first ConfigMap with the given name, or nil.
func findConfigMap(objects []unstructured.Unstructured, name string) *unstructured.Unstructured {
	for i := range objects {
		if objects[i].GetKind() == "ConfigMap" && objects[i].GetName() == name {
			return &objects[i]
		}
	}

	return nil
}

// concurrentRenderCase pairs the render-time values used by one concurrent
// worker with the output that worker must observe.
type concurrentRenderCase struct {
	values      map[string]any
	expectation dependencyExpectation
}

// concurrentRenderResult carries one worker's render outcome back to the test
// goroutine, where all assertions are made.
type concurrentRenderResult struct {
	caseIndex int
	objects   []unstructured.Unstructured
	err       error
}

// concurrentDependencyCases returns four render cases with distinct value and
// dependency settings, including one case per subchart condition.
func concurrentDependencyCases(greeting string) []concurrentRenderCase {
	return []concurrentRenderCase{
		{
			values: map[string]any{"greeting": greeting + "-1"},
			expectation: dependencyExpectation{
				childAEnabled: true,
				childBEnabled: true,
				childCEnabled: true,
				greeting:      greeting + "-1",
			},
		},
		{
			values: map[string]any{
				"greeting": greeting + "-2",
				"child-a":  map[string]any{"enabled": false},
			},
			expectation: dependencyExpectation{
				childAEnabled: false,
				childBEnabled: true,
				childCEnabled: true,
				greeting:      greeting + "-2",
			},
		},
		{
			values: map[string]any{
				"greeting": greeting + "-3",
				"child-b":  map[string]any{"enabled": false},
			},
			expectation: dependencyExpectation{
				childAEnabled: true,
				childBEnabled: false,
				childCEnabled: true,
				greeting:      greeting + "-3",
			},
		},
		{
			values: map[string]any{
				"greeting":      greeting + "-4",
				"child-c-alias": map[string]any{"enabled": false},
			},
			expectation: dependencyExpectation{
				childAEnabled: true,
				childBEnabled: true,
				childCEnabled: false,
				greeting:      greeting + "-4",
			},
		},
	}
}

// runConcurrentDependencyRound renders the renderer from several goroutines at
// once, using a start barrier so every worker is running before the first
// render begins. Every worker must observe the output its own settings
// produce; all assertions happen in the test goroutine.
func runConcurrentDependencyRound(
	t *testing.T,
	g *WithT,
	renderer *helm.Renderer,
	roundName string,
	greeting string,
) {
	t.Helper()

	cases := concurrentDependencyCases(greeting)
	results := make(chan concurrentRenderResult, len(cases))

	var barrier sync.WaitGroup
	barrier.Add(1)

	ctx := t.Context()

	var wg sync.WaitGroup
	for i := range cases {
		wg.Go(func() {
			barrier.Wait()

			objects, err := renderer.Process(ctx, cases[i].values)
			results <- concurrentRenderResult{caseIndex: i, objects: objects, err: err}
		})
	}

	barrier.Done()
	wg.Wait()
	close(results)

	seen := make(map[int]bool, len(cases))
	for result := range results {
		g.Expect(result.err).ToNot(HaveOccurred(), "round %s: render should succeed", roundName)
		assertDependencyRender(g, result.objects, "deps-concurrent", cases[result.caseIndex].expectation)
		seen[result.caseIndex] = true
	}
	g.Expect(seen).To(HaveLen(len(cases)), "round %s: every case should report exactly once", roundName)
}
