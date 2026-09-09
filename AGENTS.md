# Agent Guide: renderer-helm

`renderer-helm` renders Helm v4 charts from OCI references, Helm repositories, or local chart paths and returns Kubernetes `unstructured.Unstructured` objects.

## Documentation

- [README](README.md) — user-facing examples.
- [Design](docs/design.md) — renderer architecture and behavior.
- [Development](docs/development.md) — workflow and test guidance.

## Public API

The package is imported from `github.com/k8s-manifest-kit/renderer-helm/pkg`.

- `helm.New([]helm.Source{...}, opts...)` creates a renderer for one or more charts.
- `helm.NewEngine(source, opts...)` creates an `engine.Engine` for one chart.
- `Source` fields include `Repo`, `Chart`, optional `FS`, `ReleaseName`, `ReleaseNamespace`, `ReleaseVersion`, `Values`, `Credentials`, `ProcessDependencies`, and source-specific `PostRenderers`.
- `Values(map[string]any)` and `StaticCredentials(username, password)` provide static callbacks.
- Renderer options include filters, transformers, post-renderers, source selectors, caching, source annotations, content hashes, repository/content paths, lint mode, and strict mode.

Charts load lazily per source and are protected for concurrent use. Values are merged in this order: chart defaults, source values, then render-time values. Use specific chart versions for reproducible renders.

Source annotations and content hashes are opt-in/opt-out respectively: source annotations are disabled by default, while content hashes are enabled by default. Use the constants from `engine/pkg/types` instead of copying annotation strings.

## Development

Run commands from this directory:

```bash
make test
make fmt
make lint
make lint/fix
make check
```

Helm and registry tests may need network access. Use the checked-in test charts for deterministic tests where possible, unique valid release names, Gomega assertions, and `t.Context()`.
