package helm

import (
	"helm.sh/helm/v3/pkg/chartutil"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/pkg/util/cache"
)

// ChartSpec contains the data used to generate cache keys for rendered charts.
type ChartSpec struct {
	Chart          string
	ReleaseName    string
	ReleaseVersion string
	Values         chartutil.Values
}

// newCache creates a cache instance with Helm-specific default KeyFunc.
func newCache(opts *cache.Options) cache.Interface[[]unstructured.Unstructured] {
	if opts == nil {
		return nil
	}

	co := *opts

	// Inject default KeyFunc for Helm charts
	if co.KeyFunc == nil {
		co.KeyFunc = cache.DefaultKeyFunc
	}

	return cache.NewRenderCache(co)
}
