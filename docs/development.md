# Helm Renderer Development

## Prerequisites and commands

Use Go 1.26.8. Helm chart tests may need a chart fixture or network access
when they exercise remote repositories. The supported local workflow is:

```bash
make test
make fmt
make lint
make lint/fix
make check
```

The Makefile owns the tool versions and command details.

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
