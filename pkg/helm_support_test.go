package helm_test

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"
)

// newUnavailableRegistry starts a local HTTP server that stands in for an
// unreachable OCI registry. The OCI client always speaks TLS, so requests
// fail during the handshake against this plain-HTTP server before the
// handler is ever reached. It returns an OCI chart reference that resolves
// to no chart.
func newUnavailableRegistry(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	return "oci://" + srv.Listener.Addr().String() + "/non-existent/chart"
}

func mapChartFS(t *testing.T) fstest.MapFS {
	t.Helper()

	diskFS := os.DirFS(testChartPath)
	chartFS := fstest.MapFS{}
	err := fs.WalkDir(diskFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		data, err := fs.ReadFile(diskFS, path)
		if err != nil {
			return fmt.Errorf("failed to read %q: %w", path, err)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %q: %w", path, err)
		}

		chartFS["chart/"+path] = &fstest.MapFile{
			Data:    data,
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}

		return nil
	})
	if err != nil {
		t.Fatalf("failed to build chart filesystem: %v", err)
	}

	chartFS["chart/.helmignore"] = &fstest.MapFile{
		Data: []byte("templates/ignored.yaml\n"),
		Mode: 0o644,
	}
	chartFS["chart/templates/ignored.yaml"] = &fstest.MapFile{
		Data: []byte("{{ invalid template"),
		Mode: 0o644,
	}

	return chartFS
}
