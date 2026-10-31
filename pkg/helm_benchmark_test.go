package helm_test

import (
	"context"
	"testing"

	"github.com/rs/xid"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
)

func BenchmarkHelmRenderWithoutCache(b *testing.B) {
	renderer, err := helm.New([]helm.Source{
		{
			Chart:       testChartPath,
			ReleaseName: "bench-no-cache",
			Values: helm.Values(map[string]any{
				"replicaCount": 1,
			}),
		},
	})
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}

func BenchmarkHelmRenderWithCache(b *testing.B) {
	renderer, err := helm.New(
		[]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "bench-cache",
				Values: helm.Values(map[string]any{
					"replicaCount": 1,
				}),
			},
		},
		helm.WithCache(),
	)
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}

func BenchmarkHelmRenderCacheMiss(b *testing.B) {
	// Unique values per iteration force the default cache key to change,
	// exercising the cache-miss path on every render.
	renderer, err := helm.New(
		[]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "bench-miss",
				Values: func(_ context.Context) (types.Values, error) {
					return types.Values{
						"replicaCount": 1,
						"image": map[string]any{
							"tag": xid.New().String(),
						},
					}, nil
				},
			},
		},
		helm.WithCache(),
	)
	if err != nil {
		b.Fatalf("failed to create renderer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := renderer.Process(b.Context(), nil)
		if err != nil {
			b.Fatalf("failed to render: %v", err)
		}
	}
}
