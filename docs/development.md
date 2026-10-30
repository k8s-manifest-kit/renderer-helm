# Helm Renderer Development

## Prerequisites and commands

Use the Go version required by `go.mod`. The supported local workflow is:

```bash
make test
make test/race
make test/integration
make fmt
make lint
make lint/fix
make check
```

The Makefile owns the tool versions and command details.

## Unit and integration boundary

Unit tests (`make test`, `make test/race`) never contact external
registries or repositories. They render the checked-in test charts, use
`fstest.MapFS` and local `httptest` servers, and must stay deterministic.

Tests that exercise live OCI registries or Helm repositories are guarded
by the `integration` build tag (`//go:build integration`) and only run
through `make test/integration`, which executes every package with the
tag enabled and `-count=1` so results are always fresh. Pin the chart
inputs of these tests to exact published versions; verify a version still
exists before updating it. The tagged files are linted by
`make lint/integration`, which is part of `make check`.

## Package layout

- `pkg/helm.go` — source model and renderer orchestration.
- `pkg/helm_option.go` — renderer options and defaults.
- `pkg/helm_support.go` — chart loading, decoding, metadata, and hashes.
- `pkg/locator/` — chart and repository location handling.
- `pkg/container/` — registry/container client support.

Keep chart loading lazy and preserve per-source synchronization. Values
functions are evaluated with the render context and their errors should retain
source context. Changes to source metadata or cache keys require corresponding
tests for annotations, hashes, and cache isolation.

See [`design.md`](design.md) and [`../AGENTS.md`](../AGENTS.md).
