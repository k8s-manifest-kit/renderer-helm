package helm

import (
	"helm.sh/helm/v3/pkg/cli"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util"
	"github.com/k8s-manifest-kit/pkg/util/cache"
)

// RendererOption is a generic option for RendererOptions.
type RendererOption = util.Option[RendererOptions]

// RendererOptions is a struct-based option that can set multiple renderer options at once.
type RendererOptions struct {
	// Filters are renderer-specific filters applied during Process().
	Filters []types.Filter

	// Transformers are renderer-specific transformers applied during Process().
	Transformers []types.Transformer

	// Settings customizes the Helm environment configuration.
	// Nil means use default settings.
	Settings *cli.EnvSettings

	// Cache is a custom cache implementation for render results.
	Cache cache.Interface[[]unstructured.Unstructured]

	// SourceAnnotations enables automatic addition of source tracking annotations.
	SourceAnnotations bool

	// LintMode allows some 'required' template values to be missing without failing.
	// This is useful during linting when not all values are available.
	LintMode bool

	// Strict enables strict template rendering mode.
	// When enabled, template rendering will fail if a template references a value that was not passed in.
	Strict bool

	// CacheKeyFunc customizes how cache keys are generated from chart specifications.
	// If nil, DefaultCacheKey is used.
	CacheKeyFunc CacheKeyFunc
}

// ApplyTo applies the renderer options to the target configuration.
func (opts RendererOptions) ApplyTo(target *RendererOptions) {
	target.Filters = opts.Filters
	target.Transformers = opts.Transformers

	if opts.Settings != nil {
		target.Settings = opts.Settings
	}

	if opts.Cache != nil {
		target.Cache = opts.Cache
	}

	target.SourceAnnotations = opts.SourceAnnotations
	target.LintMode = opts.LintMode
	target.Strict = opts.Strict

	if opts.CacheKeyFunc != nil {
		target.CacheKeyFunc = opts.CacheKeyFunc
	}
}

// WithFilter adds a renderer-specific filter to this Helm renderer's processing chain.
// Renderer-specific filters are applied during Process(), before results are returned to the engine.
// For engine-level filtering applied to all renderers, use engine.WithFilter.
func WithFilter(f types.Filter) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Filters = append(opts.Filters, f)
	})
}

// WithTransformer adds a renderer-specific transformer to this Helm renderer's processing chain.
// Renderer-specific transformers are applied during Process(), before results are returned to the engine.
// For engine-level transformation applied to all renderers, use engine.WithTransformer.
func WithTransformer(t types.Transformer) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Transformers = append(opts.Transformers, t)
	})
}

// WithSettings allows customizing the Helm environment settings.
func WithSettings(settings *cli.EnvSettings) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Settings = settings
	})
}

// WithCache enables render result caching with the specified options.
// If no options are provided, uses default TTL of 5 minutes.
// By default, caching is NOT enabled.
func WithCache(opts ...cache.Option) RendererOption {
	return util.FunctionalOption[RendererOptions](func(rendererOpts *RendererOptions) {
		rendererOpts.Cache = cache.NewRenderCache(opts...)
	})
}

// WithSourceAnnotations enables or disables automatic addition of source tracking annotations.
// When enabled, the renderer adds metadata annotations to track the source type, chart, and template file.
// Annotations added: manifests.k8s-manifests-lib/source.type, source.path, source.file.
// Default: false (disabled).
func WithSourceAnnotations(enabled bool) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.SourceAnnotations = enabled
	})
}

// WithLintMode enables or disables lint mode during template rendering.
// When enabled, some 'required' template values may be missing without causing rendering to fail.
// This is useful during linting when not all values are available.
// Default: false (disabled).
func WithLintMode(enabled bool) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.LintMode = enabled
	})
}

// WithStrict enables or disables strict template rendering mode.
// When enabled, template rendering will fail if a template references a value that was not passed in.
// This helps catch missing values early during development.
// Default: false (disabled).
func WithStrict(enabled bool) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Strict = enabled
	})
}

// WithCacheKeyFunc sets a custom cache key generation function.
// Built-in options: DefaultCacheKey (default), FastCacheKey, ChartOnlyCacheKey.
//
// Example:
//
//	helm.WithCacheKeyFunc(helm.FastCacheKey)
func WithCacheKeyFunc(fn CacheKeyFunc) RendererOption {
	return util.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.CacheKeyFunc = fn
	})
}
