package helm

import (
	"slices"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/k8s-manifest-kit/pkg/util/maps"
)

// copyChart returns a render-scoped copy of the chart graph rooted at src.
//
// The renderer keeps the chart produced by the first successful load as an
// immutable source snapshot. Helm dependency processing mutates the chart it
// is given: it rewrites dependency metadata in place, removes disabled
// subcharts from the graph and from the chart metadata, and merges imported
// values into the chart defaults. Every render therefore works on its own
// copy so that repeated and concurrent renders cannot corrupt the snapshot or
// each other.
//
// The copy reproduces the source graph faithfully: dependency order (which
// drives CRD and resource emission order), parent links, metadata, lock,
// default values, schemas, templates, files, and CRDs are all preserved. File
// payloads (raw files, templates, chart files, and schemas) are shared between
// copies because the render path only reads them.
func copyChart(src *chart.Chart) *chart.Chart {
	dst := &chart.Chart{
		Raw:           slices.Clone(src.Raw),
		Metadata:      copyChartMetadata(src.Metadata),
		Lock:          copyChartLock(src.Lock),
		Templates:     slices.Clone(src.Templates),
		Values:        maps.DeepCloneMap(src.Values),
		Schema:        src.Schema,
		SchemaModTime: src.SchemaModTime,
		Files:         slices.Clone(src.Files),
		ModTime:       src.ModTime,
	}

	// AddDependency appends in order and rewires the parent links of the
	// copied nodes, so the copy keeps the source dependency order.
	for _, dependency := range src.Dependencies() {
		dst.AddDependency(copyChart(dependency))
	}

	return dst
}

// copyChartMetadata returns a copy of the chart metadata with independently
// mutable dependency entries. Dependency processing rewrites the Enabled,
// Name, and ImportValues fields of these entries in place.
func copyChartMetadata(src *chart.Metadata) *chart.Metadata {
	if src == nil {
		return nil
	}

	dst := *src
	dst.Dependencies = copyChartDependencies(src.Dependencies)

	return &dst
}

// copyChartLock returns a copy of the chart lock, preserving a nil source.
func copyChartLock(src *chart.Lock) *chart.Lock {
	if src == nil {
		return nil
	}

	dst := *src
	dst.Dependencies = copyChartDependencies(src.Dependencies)

	return &dst
}

// copyChartDependencies copies the dependency entries so in-place edits during
// dependency processing cannot reach the source snapshot. A nil slice stays
// nil: Helm distinguishes charts without dependencies from charts with an
// empty dependency list.
func copyChartDependencies(src []*chart.Dependency) []*chart.Dependency {
	if src == nil {
		return nil
	}

	dst := make([]*chart.Dependency, len(src))
	for i, dependency := range src {
		if dependency != nil {
			dependencyCopy := *dependency
			dst[i] = &dependencyCopy
		}
	}

	return dst
}
