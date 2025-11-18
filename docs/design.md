# Helm Renderer Design

## Overview

The Helm renderer provides native integration with Helm charts, enabling programmatic rendering of Helm charts from multiple sources (OCI registries, Helm repositories, local filesystem) within the k8s-manifest-kit ecosystem.

## Architecture

### Core Components

1. **Renderer** (`pkg/helm.go`)
   - Main entry point implementing `types.Renderer`
   - Manages chart loading, value merging, and template rendering
   - Handles caching at the chart-source level
   - Thread-safe for concurrent operations

2. **Source** (`pkg/helm.go`)
   - Defines chart source configuration (OCI, repository, local)
   - Specifies release name, version constraints
   - Provides dynamic value functions

3. **Options** (`pkg/helm_option.go`)
   - Functional options pattern for renderer configuration
   - Supports filters, transformers, caching, Helm settings

4. **Support** (`pkg/helm_support.go`)
   - Validation logic for sources and release names
   - Chart loading abstractions
   - Helper functions for chart discovery

5. **Engine Convenience** (`pkg/engine.go`)
   - `NewEngine()` function for simple single-chart scenarios
   - Wraps renderer creation with engine setup

## Library Design Principles

This library follows specific design principles to remain a composable, unopinionated building block for applications.

### No Logging by Design

This library intentionally does **not** include any logging functionality. This is a deliberate architectural decision based on library design best practices:

- **Libraries should not impose logging frameworks** on consuming applications
- **Log output pollutes application logs** with library-specific formatting and levels
- **The consuming application should control all logging decisions**, including when, where, and how to log
- **Avoids dependency coupling** to specific logging libraries (logrus, zap, slog, etc.)

**Instead of logging, this library provides:**
- Rich error context through Go's error wrapping (`fmt.Errorf` with `%w`)
- Clear, descriptive error messages that chain context from lower layers
- Full stack traces through wrapped errors
- Applications can inspect errors and log at their discretion

### Observability at the Right Layer

Metrics and observability concerns are **intentionally delegated to appropriate layers**:

**Cache metrics belong in the cache layer:**
- The renderer accepts `cache.Interface[[]unstructured.Unstructured]` via dependency injection
- Metrics collection is the responsibility of the cache implementation, not the renderer
- Follows the **dependency inversion principle**: renderer depends on interface, not implementation
- Users can bring their own cache with built-in metrics, tracing, or monitoring

**Why this is correct:**
- **Single Responsibility**: Renderer renders, cache caches, metrics measure
- **No coupling**: Renderer doesn't depend on metric collection strategies
- **Flexibility**: Different cache implementations can provide different observability approaches
- **Testability**: Easy to test with simple in-memory cache or sophisticated monitoring cache

### Unopinionated Library Philosophy

This library is designed to be a **focused, composable building block**:

**What this means:**
- **Does one thing well**: Renders Helm charts programmatically
- **No hidden side effects**: No file writes (except through explicit filesystem), no logging, no metrics
- **Cross-cutting concerns delegated**: Logging, metrics, tracing belong in the application layer
- **Clean interfaces**: Filesystem, cache, filters, transformers all injectable
- **Composable**: Works with any cache implementation, filesystem, or pipeline

**Benefits of this approach:**
- Library remains lightweight and focused
- No unnecessary dependencies
- Applications maintain full control over observability
- Easy to integrate into existing systems with their own observability infrastructure
- Library can be used in diverse contexts (CLI tools, web services, batch processors)

### What the Library DOES Provide

While avoiding opinions about cross-cutting concerns, the library provides everything needed for robust error handling and flexibility:

1. **Rich Error Context**
```go
return fmt.Errorf("failed to render chart %q (release %q): %w", holder.Chart, holder.ReleaseName, err)
```
- Errors chain context from lower layers
- Easy to inspect and handle at application level

2. **Clear Error Messages**
- Descriptive messages explain what failed and why
- Context includes paths, values, and operation details
- Validation errors at creation time prevent runtime surprises

3. **Interface-Based Abstractions**
- `cache.Interface`: Bring your own cache with metrics/observability
- `types.Filter` and `types.Transformer`: Inject custom processing
- `cli.EnvSettings`: Customize Helm environment configuration

4. **Functional Options Pattern**
- `WithCache()`, `WithSettings()`, `WithFilters()`, etc.
- Flexible configuration without breaking API compatibility
- Optional features remain optional

5. **No Global State**
- All configuration via constructor and options
- Thread-safe by design
- Multiple independent renderer instances coexist

This design philosophy ensures the library remains a **professional, maintainable, and composable component** suitable for production systems.

## Key Design Decisions

### 1. Chart Source Flexibility

The renderer supports three chart source types:

**OCI Registries**:
```go
Source{
    Chart: "oci://registry-1.docker.io/bitnamicharts/nginx",
    ReleaseName: "my-nginx",
}
```

**Helm Repositories**:
```go
Source{
    Repo: "https://charts.bitnami.com/bitnami",
    Chart: "nginx",
    ReleaseVersion: "15.0.0",
    ReleaseName: "my-nginx",
}
```

**Local Filesystem**:
```go
Source{
    Chart: "/path/to/chart",
    ReleaseName: "my-nginx",
}
```

**Rationale**: Different deployment scenarios require different chart sources. OCI is preferred for modern registries, repositories for traditional Helm repos, and local for development.

### 2. Lazy Chart Loading

Charts are loaded on-demand during the first `Process()` call, not at renderer creation time.

**Rationale**:
- Faster initialization when multiple renderers are configured
- Defers network I/O until actually needed
- Allows renderer configuration before chart availability is validated

**Implementation**:
```go
type Renderer struct {
    sources []Source
    once    sync.Once
    charts  []*chart.Chart
    err     error
}

func (r *Renderer) Process(ctx context.Context, values map[string]any) ([]unstructured.Unstructured, error) {
    r.once.Do(func() {
        r.charts, r.err = r.loadCharts(ctx)
    })
    // ...
}
```

### 3. Value Merging Strategy

Values are merged in this precedence (lowest to highest):
1. Chart default values (`values.yaml` in chart)
2. Source-level values (from `Source.Values` function)
3. Render-time values (passed to `Process()`)

**Implementation**:
- Uses Helm's native `chartutil.CoalesceValues()` for deep merging
- `Source.Values` function receives render-time context for dynamic values
- Final values are passed to `chartutil.ToRenderValues()` for Helm-compatible structure

**Rationale**: Provides flexibility for both static configuration (source-level) and dynamic overrides (render-time) while respecting Helm's value hierarchy.

### 4. Dependency Processing

Charts can optionally process dependencies using Helm's `chartutil.ProcessDependencies()`.

```go
Source{
    Chart: "oci://registry/my-chart",
    ProcessDependencies: true,
}
```

**Rationale**: Some charts bundle their dependencies (umbrella charts), while others expect dependencies to be pre-processed. The flag provides explicit control.

### 5. Caching Strategy

Caching is chart-source specific:

**Cache Key**: Hash of `(chart path, chart version, values)`

**Benefits**:
- Avoids re-rendering identical chart + values combinations
- Deep clones cached objects to prevent mutation
- TTL-based expiration for time-sensitive manifests

**Limitations**:
- Does not cache chart downloads (Helm SDK handles this)
- Cache is per-renderer instance (not shared across renderers)

### 6. Thread Safety

The renderer is designed for concurrent use:

**Safe Operations**:
- Multiple goroutines can call `Process()` simultaneously
- `sync.Once` ensures chart loading happens exactly once
- Each `Process()` call gets deep-cloned cache entries

**Rationale**: Enables parallel rendering in the engine when multiple Helm sources are configured.

### 7. Source Annotations

The renderer automatically adds annotations to track object provenance:

```yaml
metadata:
  annotations:
    k8s-manifest-kit/source-renderer-type: helm
    k8s-manifest-kit/source-renderer-path: oci://registry/chart
    k8s-manifest-kit/source-file: templates/deployment.yaml
```

**Rationale**: Essential for debugging multi-source deployments and tracking which chart generated which object.

### 8. Helm Environment Integration

The renderer integrates with Helm's native configuration:

```go
WithSettings(&cli.EnvSettings{
    RepositoryCache: "/custom/cache",
    // ... other settings
})
```

**Rationale**: Allows users to customize Helm behavior (caching, timeouts, registry auth) using Helm's standard configuration.

## Rendering Pipeline

1. **Initialization**: Create renderer with sources and options
2. **Lazy Loading** (on first `Process()`):
   - Download/load chart from source
   - Process dependencies if enabled
   - Cache loaded charts
3. **Value Merging** (per `Process()` call):
   - Load chart defaults
   - Call `Source.Values()` function
   - Merge with render-time values
4. **Template Rendering**:
   - Execute Helm templates with merged values
   - Parse YAML output into Kubernetes objects
5. **Filtering**: Apply renderer-specific filters
6. **Transformation**: Apply renderer-specific transformers
7. **Annotation**: Add source tracking annotations
8. **Caching**: Store results if cache is enabled

## Performance Considerations

### Chart Download Overhead

- **Mitigation**: Helm SDK caches chart downloads by default
- **Best Practice**: Use specific chart versions (not `latest`) for reproducible builds

### Template Rendering Cost

- **Mitigation**: Enable caching with appropriate TTL for repeated renders
- **Best Practice**: Use narrow `ReleaseVersion` constraints to minimize re-downloads

### Memory Usage

- **Consideration**: Each source loads a full chart into memory
- **Mitigation**: Use multiple renderers instead of multiple sources in one renderer
- **Best Practice**: Share cache across sources when values are identical

## Error Handling

### Validation Errors
- Invalid release names (length, format) fail at renderer creation
- Missing required fields fail at renderer creation

### Chart Loading Errors
- Network failures during chart download
- Invalid chart structure
- Missing dependencies

### Rendering Errors
- Template execution failures
- Invalid YAML output
- Type conversion errors

All errors are wrapped with context using `fmt.Errorf("...: %w", err)` for full stack traces.

## Integration with Engine

The Helm renderer integrates with the engine in two ways:

**Direct Renderer** (multiple sources, advanced configuration):
```go
renderer := helm.New([]helm.Source{...}, opts...)
engine := engine.New(engine.WithRenderer(renderer))
```

**Convenience Function** (single source, simple use case):
```go
engine := helm.NewEngine(helm.Source{...}, opts...)
```

Both approaches support:
- Engine-level filters and transformers
- Parallel rendering with other renderers
- Render-time value injection
- Caching and metrics collection

## Future Enhancements

### Potential Improvements

1. **Chart Validation**: Pre-validate charts against Kubernetes schemas
2. **Helm Hooks**: Support for Helm hook annotations
3. **Post-Rendering**: Support for Helm's post-rendering mechanism
4. **Secret Management**: Integration with secret providers (Vault, AWS Secrets Manager)
5. **Chart Repository Management**: Built-in repository add/update functionality
6. **Chart Testing**: Integration with Helm test capabilities

