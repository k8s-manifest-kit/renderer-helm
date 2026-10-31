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
make test/race
make test/integration
make fmt
make lint
make lint/fix
make check
```

Unit tests (`make test`, `make test/race`) are deterministic and offline:
they only use the checked-in test charts, `fstest.MapFS`, and local
`httptest` servers. Tests that contact live OCI registries or Helm
repositories are guarded by the `integration` build tag and run through
`make test/integration` (also linted via `make lint/integration`, part of
`make check`). Pin exact chart versions in integration tests so they stay
reproducible, except tests that deliberately exercise version discovery
or semver constraints. Use unique valid release names, Gomega assertions,
and `t.Context()`. See [Development](docs/development.md) for code, test,
concurrency, and cache-evidence conventions.
