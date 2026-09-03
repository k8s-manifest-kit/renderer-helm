# Helm Renderer

`renderer-helm` renders Helm v4 charts through the shared engine interfaces.
Sources may refer to local or remote charts, supply release metadata and
values, process dependencies, and attach source-specific post-renderers.

## Installation

```bash
go get github.com/k8s-manifest-kit/renderer-helm
```

## Quick start

```go
e, err := helm.NewEngine(
    helm.Source{
        Chart:       "./charts/my-app",
        ReleaseName: "my-app",
    },
)
if err != nil {
    return err
}

objects, err := e.Render(ctx)
```

Use `helm.Source.Values` for dynamic values. Engine options support filters,
transformers, post-renderers, source selection, repository configuration and
caching, source annotations, content hashes, lint mode, and strict mode.

Multiple sources can be passed to `helm.NewEngine`; render-time values are
merged with source values according to the renderer's documented precedence.
Repository and chart loading is lazy and synchronized per source.

See [`docs/design.md`](docs/design.md), [`docs/development.md`](docs/development.md),
and [`AGENTS.md`](AGENTS.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
