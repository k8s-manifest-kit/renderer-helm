package helm

import (
	"helm.sh/helm/v3/pkg/chartutil"

	"k8s.io/apimachinery/pkg/util/dump"
)

// ChartSpec contains the data used to generate cache keys for rendered charts.
type ChartSpec struct {
	Chart          string
	ReleaseName    string
	ReleaseVersion string
	Values         chartutil.Values
}

// CacheKeyFunc generates a cache key from chart specification.
type CacheKeyFunc func(ChartSpec) string

// DefaultCacheKey returns a CacheKeyFunc that uses reflection-based hashing of all chart
// specification fields. This is the safest option but may be slower for large value structures.
//
// Security Considerations:
// Cache keys are generated from chart values which may contain sensitive data such as
// passwords, API tokens, or other secrets. The resulting hash is deterministic and could
// potentially leak information if logged or exposed. For charts with sensitive values:
//   - Avoid logging cache keys in production environments
//   - Consider using FastCacheKey() or ChartOnlyCacheKey() which ignore values
//   - Implement a custom CacheKeyFunc that excludes sensitive fields
//
// Example with sensitive data:
//
//	// If your values contain secrets, consider alternative cache key strategies
//	renderer := helm.New(sources, helm.WithCacheKeyFunc(helm.FastCacheKey()))
func DefaultCacheKey() CacheKeyFunc {
	return func(spec ChartSpec) string {
		return dump.ForHash(spec)
	}
}

// FastCacheKey returns a CacheKeyFunc that generates keys based only on chart metadata,
// ignoring values. Use this when values don't affect the rendered output, when performance
// is critical, or when values may contain sensitive data that should not be included in
// cache keys.
func FastCacheKey() CacheKeyFunc {
	return func(spec ChartSpec) string {
		return spec.Chart + ":" + spec.ReleaseName + ":" + spec.ReleaseVersion
	}
}

// ChartOnlyCacheKey returns a CacheKeyFunc that generates keys based only on chart and version.
// Use this when rendering the same chart multiple times with identical values, or when you want
// maximum cache hit rates regardless of release name or values.
func ChartOnlyCacheKey() CacheKeyFunc {
	return func(spec ChartSpec) string {
		if spec.ReleaseVersion != "" {
			return spec.Chart + ":" + spec.ReleaseVersion
		}

		return spec.Chart
	}
}
