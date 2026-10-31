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

## Chart state

Charts are loaded lazily and only the first successful load is kept. That
loaded chart is an immutable source snapshot: it is never reread, refetched,
or mutated. Every render copies the chart graph — preserving dependency order
(which drives CRD and resource emission order), parent links, metadata, lock,
default values, schemas, templates, files, and CRDs — and runs dependency
processing, value coalescing, template rendering, and CRD extraction against
the copy. File payloads are shared between copies because the render path only
reads them. Dependency entries are copied with independently mutable `Tags`
and `ImportValues`: their nested data stays reachable from the copied chart
during rendering (templates can mutate nested metadata through Sprig
functions such as `set`), so sharing them would let a render mutate the
source snapshot.

The copy is made before dependency processing runs, because processing is
destructive: it rewrites dependency entries in place and removes disabled
subcharts from the graph and the chart metadata. A lock can serialize
renders, but it cannot restore a graph after processing has removed
dependencies, so isolation comes from copying the pristine snapshot rather
than from synchronization.

As a result, repeated and concurrent renders are independent: a render that
disables, enables, or aliases a subchart cannot affect the output of any other
render of the same renderer, and a request that is served from the output cache
never mutates chart state.

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

Render output caches are partitioned by source: every registered source owns an
independent cache created from the same renderer cache options. Two sources
therefore never reuse each other's output, even when their chart specs are
identical or when the configured key function — including `FastCacheKeyFunc` or
a constant custom key — maps them to the same key. Key functions only decide
reuse within a single source: a constant custom key still merges renders of the
same source, and source-specific post-renderers reapply to the cloned results
on every hit.

Cache entries expire after the configured TTL. Expiry only causes the next
render of a source to run live; the chart itself is a memoized snapshot and is
never reloaded or refetched because a cache entry expired.

See [`../AGENTS.md`](../AGENTS.md) and [`development.md`](development.md).

## OCI content integrity

OCI pulls verify every consumed document against its descriptor before it is
decoded or returned as a chart: the root manifest or index, each probed index
candidate, and the selected chart layer are downloaded and checked for an
exact size match and a matching digest, regardless of the
`Docker-Content-Digest` headers the registry serves. Substituted bytes
(including same-length, syntactically valid substitutions), truncated or
extended bodies, invalid or unsupported digests, and invalid descriptor sizes
are rejected before any content is used. Digest-pinned pulls (`PullDigest`
and `oci://` references with an embedded digest) additionally require the
resolved root descriptor to match the requested digest.

Sizes are validated on the descriptor before any bytes are fetched:
manifests and indexes are capped at 4 MiB and chart layers at 256 MiB, so a
hostile descriptor cannot drive large reads or allocations. This is byte
integrity verification against registry-served descriptors — it is not
publisher, signature, or provenance verification.
