# Helm Renderer Development Guide

## Overview

This guide covers development practices, conventions, and patterns for contributing to the Helm renderer.

## Development Setup

### Prerequisites

```bash
# Required tools
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Install dependencies
cd /path/to/renderer-helm
go mod download
```

### Running Tests

```bash
# All tests
make test

# Specific package
go test -v ./pkg

# With coverage
make test-coverage

# Watch mode (requires entr)
find . -name '*.go' | entr -c make test
```

### Linting

```bash
# Run all linters
make lint

# Auto-fix issues
make lint-fix

# Specific linter
golangci-lint run --enable-only=errcheck
```

## Code Conventions

### Go Style

**Imports**: Use `gci` formatter (runs automatically with `make lint-fix`):
```go
import (
    // Standard library
    "context"
    "fmt"
    
    // Third-party (default)
    "helm.sh/helm/v3/pkg/chart"
    
    // Kubernetes
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    
    // k8s-manifest-kit (local)
    "github.com/k8s-manifest-kit/engine/pkg/types"
    "github.com/k8s-manifest-kit/pkg/util"
    
    // Dot imports (test assertions only)
    . "github.com/onsi/gomega"
)
```

**Error Handling**: Always wrap errors with context:
```go
// Good
if err != nil {
    return nil, fmt.Errorf("failed to load chart from %s: %w", source.Chart, err)
}

// Bad
if err != nil {
    return nil, err
}
```

**Naming**:
- Receivers: Single letter or short abbreviation (`r *Renderer`, `s *Source`)
- Interfaces: `-er` suffix (e.g., `Loader`, `Validator`)
- Test functions: `Test<FunctionName>` with `t.Run()` subtests

### Functional Options Pattern

All configuration uses the functional options pattern:

```go
// Option type
type RendererOption = util.Option[RendererOptions]

// Option struct
type RendererOptions struct {
    Filters      []types.Filter
    Transformers []types.Transformer
    Cache        cache.Cache
    Settings     *cli.EnvSettings
}

// Constructor
func WithFilters(filters ...types.Filter) RendererOption {
    return util.NewOption(func(o *RendererOptions) {
        o.Filters = filters
    })
}

// Usage
renderer := helm.New(sources, 
    helm.WithFilters(gvk.Deployment()),
    helm.WithCache(cache.WithTTL(5*time.Minute)),
)
```

### Testing Practices

**Test Organization**:
```go
func TestRenderer(t *testing.T) {
    t.Run("should render chart from OCI registry", func(t *testing.T) {
        g := NewWithT(t)
        // Test implementation
    })
    
    t.Run("should handle invalid chart path", func(t *testing.T) {
        g := NewWithT(t)
        // Test implementation
    })
}
```

**Assertion Style**: Use Gomega for readable assertions:
```go
// Good
g.Expect(objects).Should(HaveLen(5))
g.Expect(err).ShouldNot(HaveOccurred())

// Avoid
if len(objects) != 5 {
    t.Fatalf("expected 5 objects, got %d", len(objects))
}
```

**Test Charts**: Use public OCI charts when possible:
```go
helm.Source{
    Chart: "oci://registry-1.docker.io/bitnamicharts/nginx",
    ReleaseName: "test-release",
}
```

**Unique Release Names**: Use `xid.New().String()` for test isolation:
```go
releaseName := fmt.Sprintf("test-%s", xid.New().String())
```

## Common Development Patterns

### Adding a New Option

1. **Add field to options struct** (`helm_option.go`):
```go
type RendererOptions struct {
    // ... existing fields
    MaxHistory int
}
```

2. **Create option constructor**:
```go
// WithMaxHistory limits the number of releases in history.
func WithMaxHistory(max int) RendererOption {
    return util.NewOption(func(o *RendererOptions) {
        o.MaxHistory = max
    })
}
```

3. **Apply in renderer** (`helm.go`):
```go
func New(sources []Source, opts ...RendererOption) (*Renderer, error) {
    options := util.ApplyOptions(&RendererOptions{
        MaxHistory: 10, // Default value
    }, opts...)
    
    // Use options.MaxHistory
}
```

4. **Add tests** (`helm_test.go`):
```go
t.Run("should respect max history option", func(t *testing.T) {
    g := NewWithT(t)
    renderer, err := helm.New(sources, helm.WithMaxHistory(5))
    g.Expect(err).ShouldNot(HaveOccurred())
    // Verify behavior
})
```

### Working with Charts

**Loading from OCI**:
```go
client, err := registry.NewClient()
if err != nil {
    return fmt.Errorf("failed to create registry client: %w", err)
}

chart, err := client.LoadChart(ctx, "oci://registry/chart:tag")
```

**Loading from Filesystem**:
```go
chart, err := loader.Load("/path/to/chart")
if err != nil {
    return fmt.Errorf("failed to load chart: %w", err)
}
```

**Processing Dependencies**:
```go
if source.ProcessDependencies {
    if err := chartutil.ProcessDependencies(chart, values); err != nil {
        return fmt.Errorf("failed to process dependencies: %w", err)
    }
}
```

### Value Merging

```go
// Start with chart defaults
vals := chart.Values

// Merge source-level values
if source.Values != nil {
    sourceVals, err := source.Values(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to get source values: %w", err)
    }
    vals = chartutil.CoalesceValues(chart, vals, sourceVals)
}

// Merge render-time values
vals = chartutil.CoalesceValues(chart, vals, renderValues)

// Convert to Helm's expected structure
renderVals, err := chartutil.ToRenderValues(chart, vals, releaseOptions, capabilities)
```

### Rendering Templates

```go
rendered, err := engine.Render(chart, renderVals)
if err != nil {
    return nil, fmt.Errorf("failed to render chart templates: %w", err)
}

// rendered is map[string]string (filename -> yaml content)
for filename, content := range rendered {
    // Parse YAML into Kubernetes objects
}
```

## Debugging Tips

### Enable Helm Debug Logging

Set environment variable:
```bash
export HELM_DEBUG=1
go test -v ./pkg
```

### Inspect Rendered Manifests

Use `t.Logf()` in tests:
```go
for _, obj := range objects {
    data, _ := json.MarshalIndent(obj.Object, "", "  ")
    t.Logf("Rendered object:\n%s", data)
}
```

### Test Chart Loading Independently

```go
settings := cli.New()
actionConfig := &action.Configuration{}
install := action.NewInstall(actionConfig)
install.ReleaseName = "test"

chart, err := install.ChartPathOptions.LocateChart("oci://registry/chart", settings)
```

## Performance Optimization

### Caching Best Practices

**Enable caching for repeated renders**:
```go
renderer := helm.New(sources, 
    helm.WithCache(cache.WithTTL(5*time.Minute)),
)
```

**Cache key considerations**:
- Chart path + version + values hash
- Use stable value ordering for consistent hashing
- Consider cache invalidation strategies for dynamic values

### Chart Download Optimization

**Use specific versions**:
```go
Source{
    Chart: "oci://registry/chart:1.2.3", // Good
    // vs
    Chart: "oci://registry/chart", // Will fetch latest every time
}
```

**Leverage Helm's cache**:
```go
WithSettings(&cli.EnvSettings{
    RepositoryCache: "/persistent/cache/dir",
})
```

## Contributing Guidelines

### Pull Request Checklist

- [ ] All tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Code coverage maintained or improved
- [ ] New functions have godoc comments
- [ ] Complex logic has inline comments
- [ ] Tests added for new features
- [ ] Examples updated if API changed

### Code Review Focus Areas

1. **Error handling**: All errors wrapped with context
2. **Thread safety**: No race conditions (verify with `go test -race`)
3. **Resource cleanup**: Charts, clients closed properly
4. **API consistency**: Follows existing patterns
5. **Documentation**: Public APIs documented

## Testing Strategy

### Unit Tests
- Test each function in isolation
- Mock external dependencies (registries, filesystem)
- Cover error paths and edge cases

### Integration Tests
- Use real Helm charts from public registries
- Test full rendering pipeline
- Verify object structure and annotations

### Performance Tests
- Benchmark chart loading and rendering
- Test caching effectiveness
- Measure memory usage for large charts

## Common Gotchas

### Chart Dependency Resolution

Helm's dependency mechanism can be confusing:
- `dependencies` in `Chart.yaml` are metadata
- Actual dependency charts must be in `charts/` subdirectory
- Use `ProcessDependencies: true` to resolve and validate

### Release Name Constraints

Helm release names have strict constraints:
- Max length: 53 characters (Kubernetes label limit)
- Allowed chars: `[a-z0-9]([-a-z0-9]*[a-z0-9])?`
- Validation happens at renderer creation

### Value Merging Precedence

Remember Helm's value precedence:
1. Chart defaults (`values.yaml`)
2. Parent chart values (if subchart)
3. Source values (our `Source.Values`)
4. Render-time values (our `Process()` parameter)

Each level can override previous levels completely or merge deeply depending on structure.

### OCI Registry Authentication

OCI registries may require authentication:
```go
// Use Helm's standard credential storage
settings := cli.New()
settings.RegistryConfig = "/path/to/registry-config.json"

renderer := helm.New(sources, helm.WithSettings(settings))
```

## Resources

- [Helm SDK Documentation](https://helm.sh/docs/topics/advanced/)
- [k8s-manifest-kit/engine API](https://pkg.go.dev/github.com/k8s-manifest-kit/engine/pkg)
- [k8s-manifest-kit/pkg Utilities](https://pkg.go.dev/github.com/k8s-manifest-kit/pkg)
- [Go Testing](https://golang.org/pkg/testing/)
- [Gomega Assertions](https://onsi.github.io/gomega/)

