package locator_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	indexPath = "/index.yaml"
	chartData = "fake-chart-archive-content"
)

const repoIndexYAML = `apiVersion: v1
entries:
  mychart:
    - version: "1.2.3"
      urls:
        - mychart-1.2.3.tgz
    - version: "1.0.0"
      urls:
        - mychart-1.0.0.tgz
    - version: "2.0.0"
      urls:
        - mychart-2.0.0.tgz
`

const crossOriginRepoIndexTmpl = `apiVersion: v1
entries:
  mychart:
    - version: "1.0.0"
      urls:
        - %s/mychart-1.0.0.tgz
`

func crossOriginRepoIndex(chartBaseURL string) string {
	return fmt.Sprintf(crossOriginRepoIndexTmpl, chartBaseURL)
}

func newChartServer(t *testing.T, chartPath string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case indexPath:
			_, _ = w.Write([]byte(repoIndexYAML))
		case chartPath:
			_, _ = w.Write([]byte(chartData))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}
