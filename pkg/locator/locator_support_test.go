package locator_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

func TestExtractChartMeta_NestedSubcharts(t *testing.T) {
	t.Parallel()

	t.Run("should pick shallowest Chart.yaml regardless of name length", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		archive := buildChartArchive(t, map[string]string{
			"longchartname/charts/sub/Chart.yaml": "name: sub\nversion: \"0.1.0\"",
			"longchartname/Chart.yaml":            "name: longchartname\nversion: \"2.0.0\"",
		})

		meta, err := locator.ExtractChartMeta(archive)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta.Name).To(Equal("longchartname"))
		g.Expect(meta.Version).To(Equal("2.0.0"))
	})

	t.Run("should prefer depth-1 over depth-2 even with shorter name at depth-2", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		archive := buildChartArchive(t, map[string]string{
			"a/charts/b/Chart.yaml": "name: b\nversion: \"0.1.0\"",
			"a/Chart.yaml":          "name: a\nversion: \"1.0.0\"",
		})

		meta, err := locator.ExtractChartMeta(archive)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta.Name).To(Equal("a"))
		g.Expect(meta.Version).To(Equal("1.0.0"))
	})

	t.Run("should handle archive with only a subchart Chart.yaml", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		archive := buildChartArchive(t, map[string]string{
			"parent/charts/child/Chart.yaml": "name: child\nversion: \"0.5.0\"",
		})

		meta, err := locator.ExtractChartMeta(archive)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta.Name).To(Equal("child"))
		g.Expect(meta.Version).To(Equal("0.5.0"))
	})
}

func TestOCILocator_NonSemverTags(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	srv := newMockOCIRegistryWithTagList(t, []string{"latest", "dev-build", "nightly"})

	oci := &locator.OCI{
		Ref:       "oci://" + srv.ref,
		CacheDir:  t.TempDir(),
		PlainHTTP: true,
	}

	_, err := oci.Locate(t.Context())
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, locator.ErrNoValidSemverTag)).To(BeTrue())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildChartArchive creates a .tgz archive in a temp directory containing
// the given files (path -> content) and returns the archive path.
func buildChartArchive(t *testing.T, files map[string]string) string {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(content)),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "chart.tgz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
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
