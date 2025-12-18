# AI Assistant Guide: Helm Renderer

This file provides guidance for AI assistants (like Claude) when working with the Helm renderer codebase.

## Project Context

**Purpose**: Helm chart renderer for the k8s-manifest-kit ecosystem, enabling programmatic rendering of Helm charts from OCI registries, Helm repositories, and local filesystem.

**Key Dependencies**:
- `github.com/k8s-manifest-kit/engine/pkg` - Core engine and type definitions
- `github.com/k8s-manifest-kit/pkg` - Shared utilities (caching, errors, options)
- `helm.sh/helm/v4` - Helm SDK for chart operations
- `k8s.io/apimachinery` - Kubernetes object handling

## Architecture Overview

### Core Types

**Source** (`pkg/helm.go`):
```go
type Source struct {
    Repo            string                                          // Helm repo URL (optional)
    Chart           string                                          // Chart path (OCI, repo, or filesystem)
    ReleaseName     string                                          // Helm release name (required)
    ReleaseVersion  string                                          // Chart version constraint (optional)
    Values          func(context.Context) (map[string]any, error)   // Dynamic values function
    ProcessDependencies bool                                        // Enable dependency processing
}
```

**Renderer** (`pkg/helm.go`):
- Implements `types.Renderer` interface
- Supports multiple sources (charts)
- Lazy chart loading with `sync.Once`
- Thread-safe concurrent rendering
- Optional caching with TTL

**Options** (`pkg/helm_option.go`):
- Filters and transformers (renderer-specific)
- Cache configuration
- Helm settings (`*cli.EnvSettings`)

### API Styles

**1. Direct Renderer** (multiple charts, advanced configuration):
```go
renderer, err := helm.New(
    []helm.Source{
        {Chart: "oci://registry/app:1.0.0", ReleaseName: "app"},
        {Chart: "oci://registry/db:2.0.0", ReleaseName: "db"},
    },
    helm.WithCache(cache.WithTTL(5*time.Minute)),
    helm.WithFilters(gvk.Deployment()),
)

engine := engine.New(engine.WithRenderer(renderer))
objects, _ := engine.Render(ctx, values)
```

**2. Convenience Function** (single chart, simple use case):
```go
engine, err := helm.NewEngine(
    helm.Source{
        Chart:       "oci://registry/app:1.0.0",
        ReleaseName: "my-app",
    },
    helm.WithCache(cache.WithTTL(5*time.Minute)),
)

objects, _ := engine.Render(ctx, values)
```

## Development Workflows

### Adding New Functionality

**1. New Option**:
- Add field to `RendererOptions` (`helm_option.go`)
- Create `With<Feature>()` constructor
- Apply in `New()` function
- Add tests in `helm_test.go`

**2. New Chart Source Type**:
- Extend `Source` struct validation in `helm_support.go`
- Implement loading logic in chart loading function
- Add tests for new source type
- Update documentation

**3. New Helm Feature**:
- Check if Helm SDK already supports it
- Add option to enable/disable feature
- Integrate into rendering pipeline
- Document usage and limitations

### Testing Guidelines

**Chart Sources**:
- Use public OCI charts: `oci://registry-1.docker.io/bitnamicharts/nginx`
- Use unique release names: `xid.New().String()`
- Test error cases with invalid charts

**Assertions**:
- Use Gomega: `g.Expect(err).ShouldNot(HaveOccurred())`
- Test object count, GVK, labels
- Verify source annotations

**Test Structure**:
```go
func TestFeature(t *testing.T) {
    t.Run("should do X when Y", func(t *testing.T) {
        g := NewWithT(t)
        // Setup, execute, assert
    })
}
```

## Common Patterns

### Value Merging

```go
// Chart defaults
vals := chart.Values

// Merge source values
if source.Values != nil {
    sourceVals, _ := source.Values(ctx)
    vals = chartutil.CoalesceValues(chart, vals, sourceVals)
}

// Merge render-time values
vals = chartutil.CoalesceValues(chart, vals, renderTimeValues)

// Convert for Helm
renderVals, _ := chartutil.ToRenderValues(chart, vals, releaseOpts, caps)
```

### Chart Loading

```go
// OCI
client, _ := registry.NewClient()
chart, _ := client.LoadChart(ctx, "oci://registry/chart:tag")

// Filesystem
chart, _ := loader.Load("/path/to/chart")

// Repository (requires chart pull first)
chart, _ := loader.LoadDir("/path/to/pulled/chart")
```

### Error Wrapping

Always wrap errors with context:
```go
if err != nil {
    return nil, fmt.Errorf("failed to render chart %s: %w", source.Chart, err)
}
```

## Key Files

- `pkg/helm.go` - Main renderer implementation
- `pkg/helm_option.go` - Functional options
- `pkg/helm_support.go` - Validation and helpers
- `pkg/helm_test.go` - Comprehensive tests
- `pkg/engine.go` - Convenience `NewEngine()` function
- `pkg/engine_test.go` - `NewEngine()` tests
- `docs/design.md` - Architecture and design decisions
- `docs/development.md` - Development guidelines

## Important Considerations

### Thread Safety
- `sync.Once` ensures chart loading happens exactly once
- Cache operations must be thread-safe (deep cloning)
- Multiple `Process()` calls can run concurrently

### Helm SDK Integration
- Use Helm's native value merging (`chartutil.CoalesceValues`)
- Respect Helm's release name constraints (53 chars, lowercase, alphanumeric + hyphens)
- Leverage Helm's chart caching mechanisms

### Performance
- Chart loading is expensive (network I/O, decompression)
- Enable caching for repeated renders
- Use specific chart versions (not `latest`)
- Consider Helm's repository cache directory

### Value Precedence
Helm merges values in this order (lowest to highest):
1. Chart defaults (`values.yaml`)
2. Source values (`Source.Values()`)
3. Render-time values (`Process()` parameter)

Later values override earlier ones (deep merge for maps, replace for primitives).

### Source Annotations
The renderer automatically adds:
- `k8s-manifest-kit/source-renderer-type: helm`
- `k8s-manifest-kit/source-renderer-path: <chart reference>`
- `k8s-manifest-kit/source-file: <template filename>`

Don't remove these; they're essential for multi-source debugging.

## Debugging Tips

### Enable Helm Debug Logging
```bash
export HELM_DEBUG=1
go test -v ./pkg
```

### Inspect Rendered Objects
```go
for _, obj := range objects {
    data, _ := json.MarshalIndent(obj.Object, "", "  ")
    t.Logf("Object:\n%s", data)
}
```

### Test Chart Loading
```go
settings := cli.New()
client, _ := registry.NewClient()
chart, _ := client.LoadChart(ctx, "oci://registry/chart")
t.Logf("Chart: %s-%s", chart.Name(), chart.Metadata.Version)
```

## Code Quality Standards

### Linting
- Run `make lint` before committing
- Fix all issues (no warnings accepted)
- Use `make lint-fix` for auto-fixable issues

### Testing
- All new functions must have tests
- Aim for >80% code coverage
- Test both success and error paths
- Use real charts from public registries

### Documentation
- All exported types/functions need godoc comments
- Complex logic needs inline comments
- Update `docs/` when adding major features

## Quick Reference

### Create Renderer
```go
renderer, err := helm.New(
    []helm.Source{{Chart: "oci://...", ReleaseName: "app"}},
    helm.WithCache(cache.WithTTL(5*time.Minute)),
)
```

### Create Engine (Convenience)
```go
engine, err := helm.NewEngine(
    helm.Source{Chart: "oci://...", ReleaseName: "app"},
    helm.WithFilters(gvk.Deployment()),
)
```

### Render
```go
objects, err := renderer.Process(ctx, map[string]any{
    "replicas": 3,
    "image": "nginx:latest",
})
```

### Value Function
```go
Source{
    Chart: "oci://registry/app",
    ReleaseName: "app",
    Values: func(ctx context.Context) (map[string]any, error) {
        return map[string]any{
            "environment": "production",
            "replicas": 5,
        }, nil
    },
}
```

## When Making Changes

1. **Read existing code first** - understand patterns before changing
2. **Follow conventions** - match existing style and structure
3. **Write tests** - for new functionality and bug fixes
4. **Update docs** - when API or behavior changes
5. **Run linter** - `make lint` before committing
6. **Check coverage** - `make test-coverage` to verify
7. **Consider performance** - especially for chart loading and caching
8. **Think about errors** - wrap with context, handle gracefully

## Resources

- [Helm SDK Docs](https://helm.sh/docs/topics/advanced/)
- [Engine Pkg](https://github.com/k8s-manifest-kit/engine)
- [Pkg Utilities](https://github.com/k8s-manifest-kit/pkg)
- [Design Doc](docs/design.md)
- [Development Guide](docs/development.md)

