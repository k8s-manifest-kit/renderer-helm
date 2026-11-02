# Helm Renderer Design

## Source model

`helm.Source` describes one chart input. It supports `Repo`, `Chart`,
`ReleaseName`, `ReleaseNamespace`, `ReleaseVersion`, a dynamic `Values`
function, optional locator `Credentials`, `ProcessDependencies`, and
source-specific `PostRenderers`.

For local or embedded chart content, `Source.FS` may provide an `io/fs.FS` and
`Source.Chart` identifies the chart root within it. The chart is loaded by
`fs.LoadChartFromFS` from
`github.com/k8s-manifest-kit/renderer-helm/pkg/fs`, which converts the
filesystem contents to Helm buffered files and loads them with Helm's
`LoadFiles` API. The
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

Both value entry points are cycle-guarded before the shared clone and merge
helpers recurse into them. Resolved source values are validated on every
render — including warm output-cache hits, since values resolve before the
cache lookup — and render-time values are validated per selected source
before the per-source clone; each rejection wraps the shared
`maps.ErrCycle`, names the chart and release, and covers the whole supported
tree even when the overlay would replace the cyclic branch. The traversal
matches the clone and merge types exactly — nested `map[string]any` and
`[]any` — so shared references that do not form cycles stay valid, while
retained values of other types, Helm template functions, and encoders are
outside the guarantee. Deselected sources neither validate render-time
values nor invoke their callbacks; the engine's global rejection is a
separate, earlier boundary.

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

### Option composition

Renderer options apply left to right. Functional options touch a single
field — `WithFilter`, `WithTransformer`, `WithPostRenderer`, and
`WithSourceSelector` append, and the boolean and path setters overwrite only
their own field — so they compose with anything around them. A
`RendererOptions` struct is stronger: it replaces the hook, selector, and
boolean fields outright (including with nil and false), while empty
repository and cache paths and a nil `CacheOptions` preserve what earlier
options configured.

A hook before a struct survives only if the struct carries it; a hook after
a struct is kept:

```go
// The struct clears the filter added before it and disables hashing:
// its zero-valued Filters and ContentHash replace the previous state.
renderer, _ := helm.New(sources,
    helm.WithFilter(keepPods),
    helm.RendererOptions{SourceAnnotations: true},
    helm.WithContentHash(true), // re-enables what the struct turned off
)

// Hook after struct: the post-renderer is appended to the struct's state.
renderer, _ := helm.New(sources,
    helm.RendererOptions{SourceAnnotations: true, ContentHash: true},
    helm.WithPostRenderer(label),
)
```

A struct that wants hashing must set `ContentHash: true` explicitly; a
`WithContentHash(true)` after the struct achieves the same. These rules
govern the renderer's own options only — the engine's additive options
(`engine.WithFilter` and friends) follow the engine's rules, not these.

### Hash stages

Both object paths a source produces — CRDs decoded from the chart and
templates decoded from the rendered output — pass through the same stage
order: source annotations are applied first (when enabled), then the content
hash, then the result is stored in the source's output cache (when enabled)
and stamped with its render origin, and only afterwards do the source,
renderer, and engine hooks run.

Consequences callers can rely on: the hash covers the complete object at
hashing time, including any metadata already present — previously applied
source annotations participate, as does any pre-existing annotation. It is
not a hash of the chart's source bytes, and it is not idempotent. The
render-origin stamp and cache retrieval never rehash, and mutations made by
later hooks need not match the stored digest. The full annotation phase
contract, including phase ordering and option reuse, is the engine's
[annotation contract](../../engine/docs/design.md#annotations); this
renderer follows it at the points described above.

## Caching and repositories

Repository configuration and repository cache paths are separate from the
render content cache. The shared cache option accepts `cache.WithTTL` and
`cache.WithKeyFunc` from `github.com/k8s-manifest-kit/pkg/util/cache`, and the
Helm cache constructor and key spec live in
`github.com/k8s-manifest-kit/renderer-helm/pkg/cache` (`cache.New`,
`cache.ChartSpec`). Cached render output is cloned at cache boundaries.

Render output caches are partitioned by source: every registered source owns an
independent cache created from the same renderer cache options. Two sources
therefore never reuse each other's output, even when their chart specs are
identical or when the configured key function — including
`cache.FastCacheKeyFunc` or
a constant custom key — maps them to the same key. Key functions only decide
reuse within a single source: a constant custom key still merges renders of the
same source, and source-specific post-renderers reapply to the cloned results
on every hit.

Cache entries expire after the configured TTL. Expiry only causes the next
render of a source to run live; the chart itself is a memoized snapshot and is
never reloaded or refetched because a cache entry expired.

Breaking change: `FastCacheKeyFunc` moved from the root `helm` package to
`github.com/k8s-manifest-kit/renderer-helm/pkg/cache` (with the former private
`chartSpec` exported as `cache.ChartSpec` and the constructor as `cache.New`).
The old root symbol is gone; renderers that passed `helm.FastCacheKeyFunc` to
`WithCache` must import the local cache package and pass
`cache.FastCacheKeyFunc` instead.

## HTTP client ownership

OCI registry operations create a fresh client from
`github.com/k8s-manifest-kit/pkg/util/httpclient` when no client is injected.
Its private transport disables keep-alives and does not retain idle
connections. The default client keeps a zero overall timeout and uses the
environment proxy, matching the previous ORAS defaults without using
`http.DefaultClient` or `http.DefaultTransport`.

Classic Helm repository downloads also use a fresh shared-factory client, then
apply their path-specific settings: a 60-second client timeout, 10-second dial
and TLS handshake timeouts, a 30-second response-header timeout, and direct
connections without an environment proxy. HTTP/2 remains disabled on this
path. Keep-alives are disabled as the intentional change; credentials,
same-origin restrictions, redirects, response bounds, and error behavior are
unchanged.

`container.WithHTTPClient`, `locator.OCI.HTTPClient`, and
`locator.Repo.HTTPClient` accept caller-owned clients. A supplied client is
passed through without first-party mutation or lifecycle management. In
particular, an injected client with a nil `Transport` retains the Go standard
library fallback; callers are responsible for its transport and lifecycle.

The production default prevents concurrent local test-server cleanup from
interfering with unrelated OCI requests through a shared default transport.
The failure mechanism was reproduced locally as
`http: CloseIdleConnections called`; historical CI logs showed only failed
boolean integrity assertions, not the underlying error, so that exact CI error
was not observed.

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
