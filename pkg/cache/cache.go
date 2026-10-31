package cache

import (
	"fmt"

	"helm.sh/helm/v4/pkg/chart/common"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	utilcache "github.com/k8s-manifest-kit/pkg/util/cache"
)

// ChartSpec contains the data used to generate cache keys for rendered charts.
// Keys are evaluated within a single source's cache: every registered source
// owns an independent output cache, so sources with identical specs never
// share entries.
type ChartSpec struct {
	Chart          string
	ReleaseName    string
	ReleaseVersion string
	Values         common.Values
}

// FastCacheKeyFunc generates cache keys based only on chart identity, ignoring values.
// This provides significantly better cache performance but means all renders of the
// same chart+release+version will share cached results regardless of values.
// Keys never cross source boundaries: each registered source owns an isolated
// output cache, so two sources with the same chart+release+version still render
// and cache their own results.
//
// Use this when:
//   - Values are static and don't change between renders
//   - You accept that value changes won't trigger re-rendering from cache
//
// Usage:
//
//	import (
//		utilcache "github.com/k8s-manifest-kit/pkg/util/cache"
//		helmcache "github.com/k8s-manifest-kit/renderer-helm/pkg/cache"
//		helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
//	)
//
//	helm.WithCache(utilcache.WithKeyFunc(helmcache.FastCacheKeyFunc))
func FastCacheKeyFunc(key any) string {
	if spec, ok := key.(ChartSpec); ok {
		return fmt.Sprintf("%s:%s:%s", spec.Chart, spec.ReleaseName, spec.ReleaseVersion)
	}

	return ""
}

// New creates a cache instance with Helm-specific default KeyFunc.
// Each call returns an independent cache: the renderer creates one per
// registered source so cached outputs never cross source boundaries.
// It returns nil when caching is disabled.
func New(opts *utilcache.Options) utilcache.Interface[[]unstructured.Unstructured] {
	if opts == nil {
		return nil
	}

	co := *opts

	// Inject default KeyFunc for Helm charts
	if co.KeyFunc == nil {
		co.KeyFunc = utilcache.DefaultKeyFunc
	}

	return utilcache.NewRenderCache(co)
}
