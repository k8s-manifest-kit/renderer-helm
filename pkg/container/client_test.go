package container_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/container"

	. "github.com/onsi/gomega"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("should create client from bare reference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient("registry.example.com/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should strip oci:// prefix", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient("oci://registry.example.com/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should strip embedded tag from reference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient("oci://registry.example.com/charts/nginx:1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept reference with port number", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient("localhost:5000/charts/nginx")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept WithCredentials option", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient(
			"registry.example.com/charts/nginx",
			container.WithCredential("user", "pass"),
		)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(client).ToNot(BeNil())
	})

	t.Run("should accept WithPlainHTTP option", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		client, err := container.NewClient(
			"registry.example.com/charts/nginx",
			container.WithPlainHTTP(true),
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

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
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

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(lastContent))
	})

	t.Run("should error when no chart layer exists", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryNoChartLayer(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("no chart layer"))
	})
}

func TestClient_PullDigest(t *testing.T) {
	t.Parallel()

	t.Run("should pull chart by manifest digest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartContent := []byte("digest-pinned-chart-content")
		srv := newMockOCIRegistry(t, chartContent)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.PullDigest(t.Context(), srv.manifestDigest)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(chartContent))
	})

	t.Run("should select last matching layer when pulling by digest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		firstContent := []byte("first-chart-digest")
		lastContent := []byte("last-chart-digest")
		srv := newMockOCIRegistryMultiLayer(t, firstContent, lastContent)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.PullDigest(t.Context(), srv.manifestDigest)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(lastContent))
	})

	t.Run("should error when no chart layer exists for digest pull", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryNoChartLayer(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.PullDigest(t.Context(), srv.manifestDigest)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("no chart layer"))
	})
}

func TestClient_PullIndex(t *testing.T) {
	t.Parallel()

	t.Run("should resolve OCI index to manifest and pull chart", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartContent := []byte("index-resolved-chart")
		srv := newMockOCIRegistryWithIndex(t, chartContent)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(chartContent))
	})

	t.Run("should skip non-chart manifest in index", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartContent := []byte("correct-chart-from-second-manifest")
		srv := newMockOCIRegistryWithMixedIndex(t, chartContent)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal(chartContent))
	})

	t.Run("should return probe error when all candidates fail", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithBrokenIndex(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("all index candidates failed"))
		g.Expect(err.Error()).To(ContainSubstring("unable to fetch manifest"))
	})

	t.Run("should return no chart layer when at least one candidate is readable", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithPartiallyBrokenIndex(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("no chart layer"))
		g.Expect(err.Error()).ToNot(ContainSubstring("all index candidates failed"))
	})
}

func TestClient_Tags(t *testing.T) {
	t.Parallel()

	t.Run("should list tags from OCI registry", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithTags(t, []string{"1.0.0", "2.0.0", "3.0.0"})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		tags, err := client.Tags(t.Context())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(tags).To(ConsistOf("1.0.0", "2.0.0", "3.0.0"))
	})

	t.Run("should convert underscore to plus in tags", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithTags(t, []string{"1.0.0_build.1", "2.0.0_rc.1"})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		tags, err := client.Tags(t.Context())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(tags).To(ConsistOf("1.0.0+build.1", "2.0.0+rc.1"))
	})
}

func TestClient_Pull_EmptyTag(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	srv := newMockOCIRegistry(t, []byte("chart"))

	client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
	g.Expect(err).ToNot(HaveOccurred())

	_, err = client.Pull(t.Context(), "")
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, container.ErrEmptyTag)).To(BeTrue())
}

func TestClient_PullDigest_InvalidDigest(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	srv := newMockOCIRegistry(t, []byte("chart"))

	client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
	g.Expect(err).ToNot(HaveOccurred())

	_, err = client.PullDigest(t.Context(), "")
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, container.ErrInvalidDigest)).To(BeTrue())
}

func TestClient_SentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("should return ErrNoChartLayer when manifest has no chart layer", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryNoChartLayer(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrNoChartLayer)).To(BeTrue())
	})

	t.Run("should return ErrEmptyIndex when index has no manifests", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newMockOCIRegistryWithEmptyIndex(t)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrEmptyIndex)).To(BeTrue())
	})
}

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
