package locator_test

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
// Mock OCI registry helper
// ---------------------------------------------------------------------------

type mockOCIServer struct {
	*httptest.Server

	ref            string
	manifestDigest digest.Digest
}

// newMockOCIRegistry spins up a lightweight mock OCI registry for locator-level
// tests (e.g. digest-pin). The full OCI client test suite and its more elaborate
// mocks live in pkg/container.
func newMockOCIRegistry(t *testing.T, chartContent []byte) *mockOCIServer {
	t.Helper()

	return newMockOCIRegistryServingChart(t, chartContent, chartContent)
}

// newMockOCIRegistryServingChart builds a mock OCI registry whose manifest
// descriptors advertise chartContent, while the blob endpoint serves
// servedChart for the chart digest. Serving divergent bytes lets tests
// exercise content verification through the public locator path.
func newMockOCIRegistryServingChart(t *testing.T, chartContent []byte, servedChart []byte) *mockOCIServer {
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

	blobs := map[digest.Digest][]byte{
		chartDigest:  servedChart,
		configDigest: configContent,
	}

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

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &mockOCIServer{
		Server:         srv,
		ref:            host + "/" + repo,
		manifestDigest: manifestDigest,
	}
}

type tagOnlyOCIServer struct {
	*httptest.Server

	ref string
}

// newMockOCIRegistryWithTagList spins up a minimal mock OCI registry that only
// serves the tag list endpoint (and the /v2/ ping). This is enough to exercise
// the resolveTag -> latestSemver path without needing manifests or blobs.
func newMockOCIRegistryWithTagList(t *testing.T, tags []string) *tagOnlyOCIServer {
	t.Helper()

	const repo = "test/chart"

	mux := http.NewServeMux()

	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

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

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host := srv.Listener.Addr().String()

	return &tagOnlyOCIServer{
		Server: srv,
		ref:    host + "/" + repo,
	}
}
