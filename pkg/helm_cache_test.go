package helm_test

import (
	"context"
	"sync"
	"testing"

	"github.com/rs/xid"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/cache"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// TestCacheSourceIsolation verifies that every registered Helm source owns an
// isolated render-output cache. Sources with identical chart paths, metadata,
// releases, and values — and even sources forced to colliding keys by
// FastCacheKeyFunc or constant custom key functions — must never reuse each
// other's cached output.
func TestCacheSourceIsolation(t *testing.T) {

	t.Run("should isolate filesystem sources with identical specs under default keys", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionFSSources("cache-iso-fs"),
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// Both sources must render their own output: the second source must
		// not reuse the first source's fresh cache entry within one render.
		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-fs", types.RenderOriginLive, "alpha", "beta")

		// Repeated renders must hit each source's own cache entry.
		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-fs", types.RenderOriginCache, "alpha", "beta")
	})

	t.Run("should isolate same-metadata charts from different repositories", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionRepoSources(t, "cache-iso-repo"),
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// Both local repositories serve the same chart name, version, and
		// values; only the served template output differs.
		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-repo", types.RenderOriginLive, "alpha", "beta")

		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-repo", types.RenderOriginCache, "alpha", "beta")
	})

	t.Run("should isolate sources under FastCacheKeyFunc", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionFSSources("cache-iso-fast"),
			helm.WithCache(cache.WithKeyFunc(helm.FastCacheKeyFunc)),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// FastCacheKeyFunc ignores values, but it must not let the identical
		// chart/release/version of the two sources share entries either.
		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-fast", types.RenderOriginLive, "alpha", "beta")

		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-fast", types.RenderOriginCache, "alpha", "beta")
	})

	t.Run("should isolate sources under constant custom keys", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionFSSources("cache-iso-constant"),
			helm.WithCache(cache.WithKeyFunc(constantCacheKey)),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// A constant key must only merge renders of the same source.
		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-constant", types.RenderOriginLive, "alpha", "beta")

		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-constant", types.RenderOriginCache, "alpha", "beta")
	})

	t.Run("should reapply source post-renderers on cache hits", func(t *testing.T) {
		g := NewWithT(t)

		sources := cacheCollisionFSSources("cache-iso-post")
		markers := []string{"alpha", "beta"}
		for i := range sources {
			sources[i].PostRenderers = []types.PostRenderer{labelObjects("cache-test/source", markers[i])}
		}

		renderer, err := helm.New(
			sources,
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-post", types.RenderOriginLive, "alpha", "beta")

		// Source post-renderers run after the per-source cache lookup, so
		// they must reapply to cloned results on every hit.
		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-post", types.RenderOriginCache, "alpha", "beta")

		for i, marker := range markers {
			g.Expect(second[i].GetLabels()).To(HaveKeyWithValue("cache-test/source", marker))
		}
	})

	t.Run("should keep sources isolated when caching is disabled", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionFSSources("cache-iso-off"),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// Without WithCache every render runs live but sources still render
		// their own output.
		first, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, first, "cache-iso-off", types.RenderOriginLive, "alpha", "beta")

		second, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, second, "cache-iso-off", types.RenderOriginLive, "alpha", "beta")
	})

	t.Run("should isolate distinct sources under concurrent renders", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New(
			cacheCollisionFSSources("cache-iso-race"),
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// Cold initialization: both charts load and both caches populate.
		cold, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, cold, "cache-iso-race", types.RenderOriginLive, "alpha", "beta")

		const workers = 8

		results := make(chan concurrentCacheResult, workers)

		var barrier sync.WaitGroup
		barrier.Add(1)

		ctx := t.Context()

		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				barrier.Wait()

				objects, err := renderer.Process(ctx, nil)

				// Mutate the returned nested objects so the test proves that
				// concurrent cache hits hand every worker an independent copy.
				for i := range objects {
					objects[i].SetLabels(map[string]string{"cache-test/worker": "mutated"})
				}

				results <- concurrentCacheResult{objects: objects, err: err}
			})
		}

		barrier.Done()
		wg.Wait()
		close(results)

		renders := 0
		for result := range results {
			g.Expect(result.err).ToNot(HaveOccurred())
			assertCollisionRender(g, result.objects, "cache-iso-race", types.RenderOriginCache, "alpha", "beta")
			renders++
		}
		g.Expect(renders).To(Equal(workers), "every worker should report exactly once")

		// Worker mutations must never reach the cached entries.
		after, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		assertCollisionRender(g, after, "cache-iso-race", types.RenderOriginCache, "alpha", "beta")

		for i := range after {
			g.Expect(after[i].GetLabels()).ToNot(HaveKey("cache-test/worker"))
		}
	})
}
func TestCacheIntegration(t *testing.T) {

	t.Run("should cache identical renders", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cache-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 2,
				}),
			},
		},
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render - cache miss
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))
		for _, obj := range result1 {
			g.Expect(obj.GetAnnotations()).Should(HaveKeyWithValue(
				types.AnnotationRenderOrigin,
				types.RenderOriginLive,
			))
		}

		// Second render - cache hit
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))

		for _, obj := range result2 {
			g.Expect(obj.GetAnnotations()).Should(HaveKeyWithValue(
				types.AnnotationRenderOrigin,
				types.RenderOriginCache,
			))
		}
	})

	t.Run("should miss cache on different values", func(t *testing.T) {
		g := NewWithT(t)
		callCount := 0
		dynamicValues := func(_ context.Context) (types.Values, error) {
			callCount++

			return types.Values{
				"replicaCount": callCount,
				"image": map[string]any{
					"tag": xid.New().String(),
				},
			}, nil
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "dynamic-cache-test",
				Values:      dynamicValues,
			},
		},
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render - cache miss
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))
		assertRenderOrigin(g, result1, types.RenderOriginLive)

		// Second render with different values - cache miss again: the default
		// key includes the rendered values, so new values must re-render.
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(3))
		assertRenderOrigin(g, result2, types.RenderOriginLive)

		// The second render must produce its own output, not a stale result.
		g.Expect(deploymentReplicas(g, result1)).To(Equal(int64(1)))
		g.Expect(deploymentReplicas(g, result2)).To(Equal(int64(2)))
	})

	t.Run("should work with cache disabled", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New(
			[]helm.Source{
				{
					Chart:       testChartPath,
					ReleaseName: "no-cache-test",
					Values: helm.Values(map[string]any{
						"replicaCount": 1,
					}),
				},
			},
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))

		// Second render - should work even without cache
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))
	})

	t.Run("should return clones from cache", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "clone-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		},
			helm.WithCache(),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render populates the cache
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))
		assertRenderOrigin(g, result1, types.RenderOriginLive)

		// Mutate the nested objects of the returned result
		for i := range result1 {
			result1[i].SetName("modified-name")
		}

		// Second render hits the cache and must be unaffected by the mutations
		result2, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(3))
		assertRenderOrigin(g, result2, types.RenderOriginCache)

		for i := range result2 {
			g.Expect(result2[i].GetName()).ToNot(Equal("modified-name"))
		}

		// Mutating a cached result must not pollute later hits either
		result2[0].SetName("modified-again")

		result3, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result3).To(HaveLen(3))
		assertRenderOrigin(g, result3, types.RenderOriginCache)
		g.Expect(result3[0].GetName()).ToNot(Equal("modified-again"))
	})

	t.Run("should use custom cache key function", func(t *testing.T) {
		g := NewWithT(t)

		// Use FastCacheKeyFunc which ignores values for better performance
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "custom-key-test",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		},
			helm.WithCache(cache.WithKeyFunc(helm.FastCacheKeyFunc)),
			helm.WithSourceAnnotations(true),
		)
		g.Expect(err).ToNot(HaveOccurred())

		// First render - cache miss
		result1, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result1).To(HaveLen(3))
		assertRenderOrigin(g, result1, types.RenderOriginLive)
		g.Expect(deploymentReplicas(g, result1)).To(Equal(int64(1)))

		// Second render with DIFFERENT values but using FastCacheKeyFunc
		// which ignores values within one source, so this is a cache hit
		result2, err := renderer.Process(t.Context(), map[string]any{
			"replicaCount": 5,
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result2).To(HaveLen(len(result1)))
		assertRenderOrigin(g, result2, types.RenderOriginCache)

		// The hit must serve the first render's output even though the second
		// render passed different values
		g.Expect(deploymentReplicas(g, result2)).To(Equal(int64(1)))
	})
}
