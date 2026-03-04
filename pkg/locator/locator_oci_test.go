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

	"github.com/k8s-manifest-kit/renderer-helm/pkg/container"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestOCILocator_RefParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     string
		version string
		wantErr string
	}{
		{
			name:    "should error on embedded tag with version",
			ref:     "oci://registry.example.com/charts/nginx:2.0.0",
			version: "1.0.0",
			wantErr: "already contains a tag",
		},
		{
			name: "should error on embedded digest with version",
			ref: "oci://registry.example.com/charts/nginx" +
				"@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			version: "1.0.0",
			wantErr: "already contains a digest",
		},
		{
			name: "should accept port with embedded tag",
			ref:  "oci://localhost:5000/charts/nginx:3.0.0",
		},
		{
			name:    "should error on invalid ref format",
			ref:     "oci://INVALID_REF:::///",
			wantErr: "invalid OCI reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			oci := &locator.OCI{
				Ref:       tt.ref,
				Version:   tt.version,
				CacheDir:  t.TempDir(),
				PlainHTTP: true,
			}

			_, err := oci.Locate(t.Context())

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))
			} else if err != nil {
				// For valid refs we only verify the ref was accepted and
				// parsed; the actual pull will fail (no real registry) but
				// the error must NOT be a parsing error.
				g.Expect(err.Error()).ToNot(ContainSubstring("invalid OCI reference"))
				g.Expect(err.Error()).ToNot(ContainSubstring("already contains"))
			}
		})
	}
}

func TestOCILocator_DigestPin(t *testing.T) {
	t.Parallel()

	t.Run("should pull chart by digest-pinned reference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartContent := []byte("digest-pinned-chart")
		srv := newMockOCIRegistry(t, chartContent)

		oci := &locator.OCI{
			Ref:       fmt.Sprintf("oci://%s@%s", srv.ref, srv.manifestDigest),
			CacheDir:  t.TempDir(),
			PlainHTTP: true,
		}

		result, err := oci.Locate(t.Context())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(MatchFields(IgnoreExtras, Fields{
			"Path":       BeARegularFile(),
			"SourceType": Equal(locator.SourceOCI),
		}))
	})

	t.Run("should error when digest and version are both specified", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		ref := "oci://registry.example.com/charts/nginx" +
			"@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            ref,
			Version:         "1.0.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("already contains a digest"))
	})
}

func TestOCILocator_Integration(t *testing.T) {
	t.Parallel()

	t.Run("should pull chart with explicit version", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "oci://registry-1.docker.io/bitnamicharts/nginx",
			Version:         "18.1.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(MatchFields(IgnoreExtras, Fields{
			"Path":       BeARegularFile(),
			"SourceType": Equal(locator.SourceOCI),
		}))

		meta, err := locator.ExtractChartMeta(result.Path)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta).To(MatchFields(IgnoreExtras, Fields{
			"Name":    Equal("nginx"),
			"Version": Equal("18.1.0"),
		}))
	})

	t.Run("should resolve latest version when version is empty", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "oci://registry-1.docker.io/bitnamicharts/nginx",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(MatchFields(IgnoreExtras, Fields{
			"Path":       BeARegularFile(),
			"SourceType": Equal(locator.SourceOCI),
		}))

		meta, err := locator.ExtractChartMeta(result.Path)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta).To(MatchFields(IgnoreExtras, Fields{
			"Name":    Equal("nginx"),
			"Version": Not(BeEmpty()),
		}))
	})
}

func TestOCILocator_DigestPullEmptyBlob(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// A zero-length chart blob produces a descriptor with Size 0,
	// which fetchBlob rejects before the locator's own empty-data guard.
	srv := newMockOCIRegistry(t, []byte{})

	oci := &locator.OCI{
		Ref:       fmt.Sprintf("oci://%s@%s", srv.ref, srv.manifestDigest),
		CacheDir:  t.TempDir(),
		PlainHTTP: true,
	}

	_, err := oci.Locate(t.Context())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(container.ErrInvalidDescriptorSize.Error())))
}

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
		chartDigest:  chartContent,
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
