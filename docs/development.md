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

The Makefile owns the tool versions and command details. `make test` and
`make test/race` run the offline unit suite (the race variant adds
`-race`). `make test/integration` runs every package with the
`integration` build tag and `-count=1`; it does not enable the race
detector. `make fmt` is the formatter and is separate from `make check`,
which runs `lint`, `lint/integration`, and `vulncheck` — it executes no
tests, so it is not a substitute for the test targets.

## Unit and integration boundary

Unit tests (`make test`, `make test/race`) never contact external
registries or repositories. They render the checked-in test charts, use
`fstest.MapFS` and local `httptest` servers, and must stay deterministic.

Tests that exercise live OCI registries or Helm repositories are guarded
by the `integration` build tag (`//go:build integration`) and only run
through `make test/integration`, which executes every package with the
tag enabled and `-count=1` so results are always fresh. Pin the chart
inputs of these tests to exact published versions; verify a version still
exists before updating it. The exceptions are the tests that deliberately
exercise version discovery or semver-constraint resolution — resolving the
latest tag of an OCI repository, or pulling through a constraint such as
`~18.1.0` — because discovery and constraint handling itself is the
behavior under test. The tagged files are linted by
`make lint/integration`, which is part of `make check`.

## Package layout

- `pkg/helm.go` — source model and renderer orchestration.
- `pkg/helm_option.go` — renderer options and defaults.
- `pkg/helm_support.go` — chart loading, decoding, metadata, and hashes.
- `pkg/fs/` — `fs.LoadChartFromFS`, `io/fs`-based chart loading.
- `pkg/helm_cache.go` — output-cache construction and cache keys.
- `pkg/helm_chart_copy.go` — render-scoped copies of the loaded chart graph.
- `pkg/locator/` — chart and repository location handling.
- `pkg/container/` — registry/container client support.

Renderer and container-client options are typed by the shared
`github.com/k8s-manifest-kit/pkg/util/option` package. The layout follows
the shared [`project-layout.md`](../../docs/project-layout.md) convention
(workspace navigation).

Keep chart loading lazy and preserve per-source synchronization. Values
functions are evaluated with the render context and their errors should retain
source context. Changes to source metadata or cache keys require corresponding
tests for annotations, hashes, and cache isolation.

## Code and test conventions

Tests live in external test packages (`helm_test`, `container_test`,
`locator_test`) and exercise the package through its public API;
`pkg/helm_support.go` holds shared implementation helpers. Keep feature
files cohesive and introduce abstractions only when they capture a real
responsibility, not to erase similar-looking code.

### Test layout

- Colocate tests with implementation. A `test/` subdirectory is a separate
  Go package, not a cosmetic folder; use existing package boundaries.
- Organize larger suites by concern using `<component>_<concern>_test.go`;
  retain a cohesive `<component>_test.go` for core API behavior. Use
  `_metadata_test.go`, `_cache_test.go`, `_values_test.go`, and
  `_concurrency_test.go` where relevant, not as mandatory files for every
  package.
- Place benchmarks in `<component>_benchmark_test.go`, in the same package
  directory, retaining benchmark names and setup/timing behavior.
- Put shared helpers in `<component>_support_test.go` and substantial
  feature-specific fixture machinery in
  `<component>_<concern>_support_test.go`. Small test-local helpers and
  scenario assertions can remain beside their tests.
- Support files contain helper/fixture/type/constant declarations, not test,
  benchmark, fuzz, or example entry points. `_support_test.go` is a naming
  convention; only `_test.go` has Go's test-file significance.
- Keep tests discoverable without excessive fragmentation: no hard line
  limit, mandatory one-file-per-test rule, shared cross-repository harness,
  fixture-storage migration, or automatic test-speed claim.
- Preserve integration build tags; filenames alone do not exclude a test
  from the offline suite. Offline cache/metrics interaction tests do not
  become live integration tests because of their function names.

Use Gomega assertions with `NewWithT(t)` — dot-imports are allowed for
Gomega — and derive contexts and temporary directories from the test with
`t.Context()` and `t.TempDir()`. Give each function parameter its own
explicit type, wrap errors with `%w` while retaining chart/release
context, and make validation errors name the failing source, key, or type
instead of echoing input contents.

### Multiline strings

Use one descriptive, standalone, package-level `const name =` declaration
with a raw string literal for every multiline string: fixtures, manifests,
templates, and expected output. Never embed multiline string literals in
function or method bodies, and never declare them as function-local
constants. Never group multiline constants in `const (...)`; related
single-line scalar constants can still be grouped, and single-line inline
strings are unaffected.

Keep large literal content separate from execution logic for readability.
Preserve meaningful content indentation; the declaration layout must not
introduce unintended bytes. A function may format the named template into
a local variable — the prohibition concerns where the literal is declared,
not where its string value is used:

```go
const fixtureYAML = `apiVersion: v1
kind: ConfigMap
`
```

## Concurrency and cache evidence

`Renderer.Process` is safe for concurrent use, but user-supplied code —
values functions, credentials, cache key functions, and post-renderers —
runs during renders and must be safe when renders overlap; the renderer
does not synchronize it. Concurrency tests use a start barrier, join
every worker, and assert on the test goroutine; each worker owns its
inputs and results. Do not use sleeps to manufacture ordering.

Cache behavior is proven through render origins, not callback counts:
enable `helm.WithSourceAnnotations(true)` and assert
`types.RenderOriginLive` on cold renders and `types.RenderOriginCache`
on warm renders. Cover constant custom keys and `FastCacheKeyFunc`
within a single source, prove nested-result independence by mutating
returned objects and re-rendering, and check that source post-renderers
reapply on cache hits. TTL expiry only forces the next render of a
source to run live; it never reloads the memoized chart.

## Regression anchors

- `TestRenderer_DependencyToggle`, `TestRenderer_DependencyEmissionOrder`,
  and `TestRenderer_ImportValuesSnapshotIsolation`
  (`pkg/helm_dependencies_test.go`) guard chart-copy isolation. Emission
  order must be asserted on rendered output order — reconstructing the
  chart graph from loader input is not sufficient evidence that order was
  preserved.
- `TestCacheSourceIsolation` (`pkg/helm_cache_test.go`) guards per-source
  output-cache isolation, including colliding custom keys and concurrent
  renders.
- `pkg/container/client_integrity_test.go` guards OCI integrity against
  hostile registries using local fake HTTP servers: substituted bytes
  (including same-length, syntactically valid substitutions), truncated or
  extended bodies, and invalid digests or descriptor sizes must be
  rejected even when the served headers look plausible.

See [`design.md`](design.md) and [`../AGENTS.md`](../AGENTS.md).
