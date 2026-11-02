package helm

import (
	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/cache"
	"github.com/k8s-manifest-kit/pkg/util/option"
)

// RendererOption is a generic option for RendererOptions.
type RendererOption = option.Option[RendererOptions]

// RendererOptions is a struct-based option that can set multiple renderer options at once.
//
// Options apply left to right: each option sees the state left by the ones
// before it. When RendererOptions is applied through ApplyTo, fields divide
// into replacing and preserving groups:
//
//   - Filters, Transformers, PostRenderers, and SourceSelectors replace the
//     target's slices, including when nil — a struct clears hooks and
//     selectors that earlier options added.
//   - SourceAnnotations, ContentHash, LintMode, and Strict replace the
//     target's booleans, including when false.
//   - Empty RepositoryConfig, RepositoryCache, and ContentCache preserve the
//     target's paths; only non-empty paths overwrite them.
//   - A nil CacheOptions preserves the target's cache configuration; a
//     non-nil CacheOptions applies its own field rules to the target's cache
//     options, creating them if needed.
//
// Functional options change only their own field or append to a single
// slice, so they compose with anything applied before or after. Note the
// difference from a fresh constructor: New starts from defaults with content
// hashing enabled, the Helm user paths for the caches, and caching disabled
// (nil cache options), while applying a RendererOptions struct replaces the
// hook, selector, and boolean fields outright — a caller who wants content
// hashing after a struct must set ContentHash: true in the struct (or follow
// it with WithContentHash(true)).
type RendererOptions struct {
	// Filters are renderer-specific filters applied during Process().
	Filters []types.Filter

	// Transformers are renderer-specific transformers applied during Process().
	Transformers []types.Transformer

	// PostRenderers are renderer-specific post-renderers applied during Process().
	PostRenderers []types.PostRenderer

	// SourceSelectors are renderer-specific source selectors evaluated before rendering each source.
	SourceSelectors []SourceSelector

	// RepositoryConfig is the path to the repositories file. Applying a
	// RendererOptions struct preserves the previous path when this field is
	// empty and overwrites it only when non-empty.
	RepositoryConfig string

	// RepositoryCache is the path to the repository cache directory.
	// Applying a RendererOptions struct preserves the previous path when
	// this field is empty and overwrites it only when non-empty.
	RepositoryCache string

	// ContentCache is the path to the content-addressable cache directory.
	// Applying a RendererOptions struct preserves the previous path when
	// this field is empty and overwrites it only when non-empty.
	ContentCache string

	// CacheOptions holds cache configuration. nil = caching disabled.
	// Applying a RendererOptions struct with a nil CacheOptions preserves
	// the previously configured cache (including disabled); a non-nil value
	// applies its field rules on top of the previous configuration.
	CacheOptions *cache.Options

	// SourceAnnotations enables automatic addition of source tracking annotations.
	// Applying a RendererOptions struct replaces this field, including with false.
	SourceAnnotations bool

	// ContentHash enables automatic addition of a SHA-256 content hash annotation.
	// Default: true (enabled) in a fresh constructor; applying a
	// RendererOptions struct replaces this field, including with false, so a
	// struct that wants hashing must set it to true explicitly.
	ContentHash bool

	// LintMode allows some 'required' template values to be missing without failing.
	// This is useful during linting when not all values are available.
	LintMode bool

	// Strict enables strict template rendering mode.
	// When enabled, template rendering will fail if a template references a value that was not passed in.
	Strict bool
}

// ApplyTo applies the renderer options to the target configuration.
//
// Application follows the composition rules documented on RendererOptions:
// hooks, selectors, and booleans replace their target fields (including
// nil and false), empty repository and cache paths preserve the target's
// paths, and a nil CacheOptions preserves the target's cache configuration
// while a non-nil one applies its field rules on top.
func (opts RendererOptions) ApplyTo(target *RendererOptions) {
	target.Filters = opts.Filters
	target.Transformers = opts.Transformers
	target.PostRenderers = opts.PostRenderers
	target.SourceSelectors = opts.SourceSelectors

	if opts.RepositoryConfig != "" {
		target.RepositoryConfig = opts.RepositoryConfig
	}

	if opts.RepositoryCache != "" {
		target.RepositoryCache = opts.RepositoryCache
	}

	if opts.ContentCache != "" {
		target.ContentCache = opts.ContentCache
	}

	if opts.CacheOptions != nil {
		if target.CacheOptions == nil {
			target.CacheOptions = &cache.Options{}
		}
		opts.CacheOptions.ApplyTo(target.CacheOptions)
	}

	target.SourceAnnotations = opts.SourceAnnotations
	target.ContentHash = opts.ContentHash
	target.LintMode = opts.LintMode
	target.Strict = opts.Strict
}

// WithFilter adds a renderer-specific filter to this Helm renderer's processing chain.
func WithFilter(f types.Filter) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Filters = append(opts.Filters, f)
	})
}

// WithTransformer adds a renderer-specific transformer to this Helm renderer's processing chain.
func WithTransformer(t types.Transformer) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Transformers = append(opts.Transformers, t)
	})
}

// WithPostRenderer adds a renderer-specific post-renderer to this Helm renderer's processing chain.
// Post-renderers run after all sources have been rendered and combined, after Filters and Transformers.
func WithPostRenderer(p types.PostRenderer) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.PostRenderers = append(opts.PostRenderers, p)
	})
}

// WithSourceSelector adds a source selector to this Helm renderer.
// Source selectors are evaluated before rendering each source. If any selector
// returns false, the source is skipped entirely.
func WithSourceSelector(s SourceSelector) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.SourceSelectors = append(opts.SourceSelectors, s)
	})
}

// WithRepositoryConfig sets the path to the repositories file.
func WithRepositoryConfig(path string) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.RepositoryConfig = path
	})
}

// WithRepositoryCache sets the path to the repository cache directory.
func WithRepositoryCache(path string) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.RepositoryCache = path
	})
}

// WithContentCache sets the path to the content-addressable cache directory.
func WithContentCache(path string) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.ContentCache = path
	})
}

// WithCache enables render result caching with the specified options.
func WithCache(opts ...cache.Option) RendererOption {
	return option.FunctionalOption[RendererOptions](func(rendererOpts *RendererOptions) {
		if rendererOpts.CacheOptions == nil {
			rendererOpts.CacheOptions = &cache.Options{}
		}

		for _, opt := range opts {
			opt.ApplyTo(rendererOpts.CacheOptions)
		}
	})
}

// WithSourceAnnotations enables or disables automatic addition of source tracking annotations.
func WithSourceAnnotations(enabled bool) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.SourceAnnotations = enabled
	})
}

// WithContentHash enables or disables automatic addition of a SHA-256 content hash annotation.
func WithContentHash(enabled bool) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.ContentHash = enabled
	})
}

// WithLintMode enables or disables lint mode during template rendering.
func WithLintMode(enabled bool) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.LintMode = enabled
	})
}

// WithStrict enables or disables strict template rendering mode.
func WithStrict(enabled bool) RendererOption {
	return option.FunctionalOption[RendererOptions](func(opts *RendererOptions) {
		opts.Strict = enabled
	})
}
