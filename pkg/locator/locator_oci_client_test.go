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

	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

func TestNewOCIClient(t *testing.T) {
	t.Parallel()

	t.Run("should create client from bare reference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient("registry.example.com/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should strip oci:// prefix", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient("oci://registry.example.com/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should strip embedded tag from reference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient("oci://registry.example.com/charts/nginx:1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept reference with port number", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient("localhost:5000/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept WithCredentials option", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient(
			"registry.example.com/charts/nginx",
			locator.WithCredentials(&locator.Credentials{Username: "user", Password: "pass"}),
		)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept WithPlainHTTP option", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := locator.NewOCIClient(
			"registry.example.com/charts/nginx",
			locator.WithPlainHTTP(true),
		)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})
}

func TestClient_Pull(t *testing.T) {
	t.Parallel()

	t.Run("should pull chart layer from OCI registry", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartContent := []byte("fake-chart-tgz-content")
		srv := newMockOCIRegistry(t, chartContent)

		client, err := locator.NewOCIClient(srv.ref, locator.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(chartContent))
	})

	t.Run("should select last matching layer when multiple chart layers exist", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		firstContent := []byte("first-chart")
		lastContent := []byte("last-chart")
		srv := newMockOCIRegistryMultiLayer(t, firstContent, lastContent)

		client, err := locator.NewOCIClient(srv.ref, locator.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(lastContent))
	})

	t.Run("should error when no chart layer exists", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryNoChartLayer(t)

		client, err := locator.NewOCIClient(srv.ref, locator.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("no chart layer"))
	})
}

func TestClient_Tags(t *testing.T) {
	t.Parallel()

	t.Run("should list tags from OCI registry", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithTags(t, []string{"1.0.0", "2.0.0", "3.0.0"})

		client, err := locator.NewOCIClient(srv.ref, locator.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		tags, err := client.Tags(t.Context())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(tags).To(ConsistOf("1.0.0", "2.0.0", "3.0.0"))
	})

	t.Run("should convert underscore to plus in tags", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithTags(t, []string{"1.0.0_build.1", "2.0.0_rc.1"})

		client, err := locator.NewOCIClient(srv.ref, locator.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		tags, err := client.Tags(t.Context())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(tags).To(ConsistOf("1.0.0+build.1", "2.0.0+rc.1"))
	})
}

type mockOCIServer struct {
	*httptest.Server

	ref string
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
		Server: srv,
		ref:    host + "/" + repo,
	}
}
