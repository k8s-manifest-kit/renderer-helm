# Helm Renderer Design

## Source model

`helm.Source` describes one chart input. It supports `Repo`, `Chart`,
`ReleaseName`, `ReleaseNamespace`, `ReleaseVersion`, a dynamic `Values`
function, optional locator `Credentials`, `ProcessDependencies`, and
source-specific `PostRenderers`.

For local or embedded chart content, `Source.FS` may provide an `io/fs.FS` and
`Source.Chart` identifies the chart root within it. The filesystem contents are
converted to Helm buffered files and loaded with Helm's `LoadFiles` API. The
existing path, repository, and OCI loading flows remain unchanged when `FS` is
nil.

`helm.NewEngine` accepts one or more sources. `SourceSelector` functions can
exclude sources before rendering. Source loading and chart discovery are lazy
and synchronized per source, allowing independent sources to be loaded safely
under concurrent renders.

## Values and rendering

Source values are resolved for each render and merged with render-time
`types.Values`; render-time values overlay source values. Helm then converts
the merged values with Helm's render-value processing and renders the chart
with the configured release name and namespace.

When `ProcessDependencies` is enabled, Helm dependency processing runs before
template rendering. Lint and strict modes are renderer options and are passed
to the Helm engine behavior configured by the renderer.

## Pipeline and output metadata

After a source is rendered, its source-specific post-renderers run. Outputs
from selected sources are combined, then renderer-level filters, transformers,
and post-renderers run through the shared engine pipeline.

Source annotations are disabled by default. When enabled, the shared source
annotation keys describe the source type and chart location. Content hashes
use `manifests.k8s-manifests-kit/content.hash` and are enabled by default.

## Caching and repositories

Repository configuration and repository cache paths are separate from the
render content cache. The shared cache option accepts `cache.WithTTL` and
`cache.WithKeyFunc`. Cached render output is cloned at cache boundaries.

See [`../AGENTS.md`](../AGENTS.md) and [`development.md`](development.md).
