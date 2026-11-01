package container_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ---------------------------------------------------------------------------
// Mock OCI registry helpers
// ---------------------------------------------------------------------------

type mockOCIServer struct {
	*httptest.Server

	ref            string
	manifestDigest digest.Digest
}

func newMockOCIRegistry(t *testing.T, chartContent []byte) *mockOCIServer {
	t.Helper()

	chartDigest := digest.FromBytes(chartContent)
	configContent := []byte(`{"name":"test-chart","version":"1.0.0"}`)
	configDigest := digest.FromBytes(configContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
		Layers: []ocispec.Descriptor{{
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Digest:    chartDigest,
			Size:      int64(len(chartContent)),
		}},
	}

	return newMockOCIServer(t, manifest, map[digest.Digest][]byte{
		chartDigest:  chartContent,
		configDigest: configContent,
	}, nil)
}

func newMockOCIRegistryMultiLayer(t *testing.T, firstContent []byte, lastContent []byte) *mockOCIServer {
	t.Helper()

	firstDigest := digest.FromBytes(firstContent)
	lastDigest := digest.FromBytes(lastContent)
	configContent := []byte(`{}`)
	configDigest := digest.FromBytes(configContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				Digest:    firstDigest,
				Size:      int64(len(firstContent)),
			},
			{
				MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				Digest:    lastDigest,
				Size:      int64(len(lastContent)),
			},
		},
	}

	return newMockOCIServer(t, manifest, map[digest.Digest][]byte{
		firstDigest:  firstContent,
		lastDigest:   lastContent,
		configDigest: configContent,
	}, nil)
}

func newMockOCIRegistryNoChartLayer(t *testing.T) *mockOCIServer {
	t.Helper()

	configContent := []byte(`{}`)
	configDigest := digest.FromBytes(configContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
		Layers: []ocispec.Descriptor{},
	}

	return newMockOCIServer(t, manifest, map[digest.Digest][]byte{
		configDigest: configContent,
	}, nil)
}

func newMockOCIRegistryWithIndex(t *testing.T, chartContent []byte) *mockOCIServer {
	t.Helper()

	chartDigest := digest.FromBytes(chartContent)
	configContent := []byte(`{"name":"test-chart","version":"1.0.0"}`)
	configDigest := digest.FromBytes(configContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
		Layers: []ocispec.Descriptor{{
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Digest:    chartDigest,
			Size:      int64(len(chartContent)),
		}},
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	manifestDigest := digest.FromBytes(manifestBytes)

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    manifestDigest,
			Size:      int64(len(manifestBytes)),
		}},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/v2/%s/manifests/", repo), func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/manifests/", repo)):]
		if ref == manifestDigest.String() {
			w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
			w.Header().Set("Docker-Content-Digest", manifestDigest.String())
			w.Header().Set("Content-Length", strconv.Itoa(len(manifestBytes)))
			_, _ = w.Write(manifestBytes)

			return
		}

		w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
		w.Header().Set("Docker-Content-Digest", indexDigest.String())
		w.Header().Set("Content-Length", strconv.Itoa(len(indexBytes)))
		_, _ = w.Write(indexBytes)
	})

	blobs := map[digest.Digest][]byte{
		chartDigest:  chartContent,
		configDigest: configContent,
	}

	mux.HandleFunc(fmt.Sprintf("/v2/%s/blobs/", repo), func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/blobs/", repo)):]

		d, err := digest.Parse(ref)
		if err != nil {
			http.Error(w, "bad digest", http.StatusBadRequest)

			return
		}

		data, ok := blobs[d]
		if !ok {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Docker-Content-Digest", d.String())
		_, _ = w.Write(data)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: indexDigest,
	}
}

func newMockOCIRegistryWithMixedIndex(t *testing.T, chartContent []byte) *mockOCIServer {
	t.Helper()

	nonChartConfig := []byte(`{"type":"not-a-chart"}`)
	nonChartConfigDigest := digest.FromBytes(nonChartConfig)
	nonChartManifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    nonChartConfigDigest,
			Size:      int64(len(nonChartConfig)),
		},
		Layers: []ocispec.Descriptor{},
	}

	nonChartBytes, err := json.Marshal(nonChartManifest)
	if err != nil {
		t.Fatal(err)
	}

	nonChartDigest := digest.FromBytes(nonChartBytes)

	chartDigest := digest.FromBytes(chartContent)
	chartConfig := []byte(`{"name":"test-chart","version":"1.0.0"}`)
	chartConfigDigest := digest.FromBytes(chartConfig)

	chartManifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    chartConfigDigest,
			Size:      int64(len(chartConfig)),
		},
		Layers: []ocispec.Descriptor{{
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Digest:    chartDigest,
			Size:      int64(len(chartContent)),
		}},
	}

	chartManifestBytes, err := json.Marshal(chartManifest)
	if err != nil {
		t.Fatal(err)
	}

	chartManifestDigest := digest.FromBytes(chartManifestBytes)

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    nonChartDigest,
				Size:      int64(len(nonChartBytes)),
			},
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    chartManifestDigest,
				Size:      int64(len(chartManifestBytes)),
			},
		},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)

	manifests := map[digest.Digest][]byte{
		nonChartDigest:      nonChartBytes,
		chartManifestDigest: chartManifestBytes,
	}

	blobs := map[digest.Digest][]byte{
		chartDigest:          chartContent,
		chartConfigDigest:    chartConfig,
		nonChartConfigDigest: nonChartConfig,
	}

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(
		fmt.Sprintf("/v2/%s/manifests/", repo),
		func(w http.ResponseWriter, r *http.Request) {
			ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/manifests/", repo)):]
			if m, ok := manifests[digest.Digest(ref)]; ok {
				w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
				w.Header().Set("Docker-Content-Digest", ref)
				w.Header().Set("Content-Length", strconv.Itoa(len(m)))
				_, _ = w.Write(m)

				return
			}

			w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
			w.Header().Set("Docker-Content-Digest", indexDigest.String())
			w.Header().Set("Content-Length", strconv.Itoa(len(indexBytes)))
			_, _ = w.Write(indexBytes)
		},
	)

	mux.HandleFunc(
		fmt.Sprintf("/v2/%s/blobs/", repo),
		func(w http.ResponseWriter, r *http.Request) {
			ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/blobs/", repo)):]

			d, err := digest.Parse(ref)
			if err != nil {
				http.Error(w, "bad digest", http.StatusBadRequest)

				return
			}

			data, ok := blobs[d]
			if !ok {
				http.NotFound(w, r)

				return
			}

			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Docker-Content-Digest", d.String())
			_, _ = w.Write(data)
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: indexDigest,
	}
}

func newMockOCIRegistryWithBrokenIndex(t *testing.T) *mockOCIServer {
	t.Helper()

	fakeDigest1 := digest.FromString("broken-manifest-1")
	fakeDigest2 := digest.FromString("broken-manifest-2")

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    fakeDigest1,
				Size:      100,
			},
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    fakeDigest2,
				Size:      100,
			},
		},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(
		fmt.Sprintf("/v2/%s/manifests/", repo),
		func(w http.ResponseWriter, r *http.Request) {
			ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/manifests/", repo)):]

			if ref == fakeDigest1.String() || ref == fakeDigest2.String() {
				http.Error(w, "internal server error", http.StatusInternalServerError)

				return
			}

			w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
			w.Header().Set("Docker-Content-Digest", indexDigest.String())
			w.Header().Set("Content-Length", strconv.Itoa(len(indexBytes)))
			_, _ = w.Write(indexBytes)
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: indexDigest,
	}
}

func newMockOCIRegistryWithPartiallyBrokenIndex(t *testing.T) *mockOCIServer {
	t.Helper()

	fakeDigest := digest.FromString("broken-manifest")

	nonChartConfig := []byte(`{"type":"not-a-chart"}`)
	nonChartConfigDigest := digest.FromBytes(nonChartConfig)
	nonChartManifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    nonChartConfigDigest,
			Size:      int64(len(nonChartConfig)),
		},
		Layers: []ocispec.Descriptor{},
	}

	nonChartBytes, err := json.Marshal(nonChartManifest)
	if err != nil {
		t.Fatal(err)
	}

	nonChartDigest := digest.FromBytes(nonChartBytes)

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    fakeDigest,
				Size:      100,
			},
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    nonChartDigest,
				Size:      int64(len(nonChartBytes)),
			},
		},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)

	manifests := map[digest.Digest][]byte{
		nonChartDigest: nonChartBytes,
	}

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(
		fmt.Sprintf("/v2/%s/manifests/", repo),
		func(w http.ResponseWriter, r *http.Request) {
			ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/manifests/", repo)):]

			if ref == fakeDigest.String() {
				http.Error(w, "internal server error", http.StatusInternalServerError)

				return
			}

			if m, ok := manifests[digest.Digest(ref)]; ok {
				w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
				w.Header().Set("Docker-Content-Digest", ref)
				w.Header().Set("Content-Length", strconv.Itoa(len(m)))
				_, _ = w.Write(m)

				return
			}

			w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
			w.Header().Set("Docker-Content-Digest", indexDigest.String())
			w.Header().Set("Content-Length", strconv.Itoa(len(indexBytes)))
			_, _ = w.Write(indexBytes)
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: indexDigest,
	}
}

func newMockOCIRegistryWithEmptyIndex(t *testing.T) *mockOCIServer {
	t.Helper()

	idx := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/v2/%s/manifests/", repo), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
		w.Header().Set("Docker-Content-Digest", indexDigest.String())
		w.Header().Set("Content-Length", strconv.Itoa(len(indexBytes)))
		_, _ = w.Write(indexBytes)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: indexDigest,
	}
}

func newMockOCIRegistryWithTags(t *testing.T, tags []string) *mockOCIServer {
	t.Helper()

	configContent := []byte(`{}`)
	configDigest := digest.FromBytes(configContent)

	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
	}

	return newMockOCIServer(t, manifest, map[digest.Digest][]byte{
		configDigest: configContent,
	}, tags)
}

func newMockOCIServer(
	t *testing.T,
	manifest ocispec.Manifest,
	blobs map[digest.Digest][]byte,
	tags []string,
) *mockOCIServer {
	t.Helper()

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	manifestDigest := digest.FromBytes(manifestBytes)

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/v2/%s/manifests/", repo), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", manifestDigest.String())
		w.Header().Set("Content-Length", strconv.Itoa(len(manifestBytes)))
		_, _ = w.Write(manifestBytes)
	})

	mux.HandleFunc(fmt.Sprintf("/v2/%s/blobs/", repo), func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Path[len(fmt.Sprintf("/v2/%s/blobs/", repo)):]
		d, err := digest.Parse(ref)
		if err != nil {
			http.Error(w, "bad digest", http.StatusBadRequest)

			return
		}

		data, ok := blobs[d]
		if !ok {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Docker-Content-Digest", d.String())
		_, _ = w.Write(data)
	})

	if tags != nil {
		mux.HandleFunc(fmt.Sprintf("/v2/%s/tags/list", repo), func(w http.ResponseWriter, _ *http.Request) {
			resp := struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}{
				Name: repo,
				Tags: tags,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: manifestDigest,
	}
}
