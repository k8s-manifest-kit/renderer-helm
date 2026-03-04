package locator_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
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

func TestLocate(t *testing.T) {
	t.Parallel()

	t.Run("should error on nil request", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		_, err := locator.Locate(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, locator.ErrNilRequest)).To(BeTrue())
	})

	t.Run("should resolve existing local directory", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartDir := filepath.Join(t.TempDir(), "mychart")
		g.Expect(os.MkdirAll(chartDir, 0750)).To(Succeed())

		result, err := locator.Locate(t.Context(), &locator.Request{Name: chartDir})
		g.Expect(err).ToNot(HaveOccurred())

		absExpected, _ := filepath.Abs(chartDir)
		g.Expect(result).To(MatchFields(IgnoreExtras, Fields{
			"Path":       Equal(absExpected),
			"SourceType": Equal(locator.SourceLocal),
		}))
	})

	t.Run("should resolve existing local file", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		tmpFile := filepath.Join(t.TempDir(), "mychart.tgz")
		g.Expect(os.WriteFile(tmpFile, []byte("fake-chart"), 0600)).To(Succeed())

		result, err := locator.Locate(t.Context(), &locator.Request{Name: tmpFile})
		g.Expect(err).ToNot(HaveOccurred())

		absExpected, _ := filepath.Abs(tmpFile)
		g.Expect(result.Path).To(Equal(absExpected))
	})

	t.Run("should error for nonexistent absolute path", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		_, err := locator.Locate(t.Context(), &locator.Request{Name: "/nonexistent/chart/path"})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("not found"))
	})

	t.Run("should error for nonexistent relative dot path", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		_, err := locator.Locate(t.Context(), &locator.Request{Name: "./nonexistent/chart"})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("not found"))
	})

	t.Run("should trim whitespace from name", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartDir := filepath.Join(t.TempDir(), "mychart")
		g.Expect(os.MkdirAll(chartDir, 0750)).To(Succeed())

		result, err := locator.Locate(t.Context(), &locator.Request{Name: "  " + chartDir + "  "})
		g.Expect(err).ToNot(HaveOccurred())

		absExpected, _ := filepath.Abs(chartDir)
		g.Expect(result.Path).To(Equal(absExpected))
	})

	t.Run("should skip local resolution when RepoURL is set", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartDir := filepath.Join(t.TempDir(), "mychart")
		g.Expect(os.MkdirAll(chartDir, 0750)).To(Succeed())

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            chartDir,
			RepoURL:         "https://charts.example.com",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).ToNot(ContainSubstring("not found"))
	})

	t.Run("should propagate credential errors", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		credErr := errors.New("vault unavailable")

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "somechart",
			RepoURL:         "https://charts.example.com",
			RepositoryCache: t.TempDir(),
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				return nil, credErr
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, credErr)).To(BeTrue())
	})

	t.Run("should propagate credential errors for OCI charts", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		credErr := errors.New("vault unavailable")

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "oci://registry-1.docker.io/bitnamicharts/nginx",
			RepositoryCache: t.TempDir(),
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				return nil, credErr
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, credErr)).To(BeTrue())
	})

	t.Run("should handle nil credential return", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartDir := filepath.Join(t.TempDir(), "mychart")
		g.Expect(os.MkdirAll(chartDir, 0750)).To(Succeed())

		result, err := locator.Locate(t.Context(), &locator.Request{
			Name: chartDir,
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				return nil, nil //nolint:nilnil // testing nil credential response
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		absExpected, _ := filepath.Abs(chartDir)
		g.Expect(result.Path).To(Equal(absExpected))
	})

	t.Run("should not call credentials for local paths", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartDir := filepath.Join(t.TempDir(), "mychart")
		g.Expect(os.MkdirAll(chartDir, 0750)).To(Succeed())

		called := false
		result, err := locator.Locate(t.Context(), &locator.Request{
			Name: chartDir,
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				called = true

				return &locator.Credentials{Username: "u", Password: "p"}, nil
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(called).To(BeFalse())

		absExpected, _ := filepath.Abs(chartDir)
		g.Expect(result.Path).To(Equal(absExpected))
	})
}

func TestRepoLocator_Download(t *testing.T) {
	t.Parallel()

	t.Run("should download chart from repo with exact version", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newChartServer(t, "/mychart-1.2.3.tgz")
		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         srv.URL,
			Version:         "1.2.3",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result).To(MatchFields(IgnoreExtras, Fields{
			"Path":       BeARegularFile(),
			"SourceType": Equal(locator.SourceRepo),
		}))

		data, err := os.ReadFile(result.Path)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(data).To(Equal([]byte(chartData)))
	})

	t.Run("should download latest version when version is empty", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newChartServer(t, "/mychart-2.0.0.tgz")
		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         srv.URL,
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.Path).To(BeARegularFile())
	})

	t.Run("should cache downloaded chart by content hash", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newChartServer(t, "/mychart-1.0.0.tgz")
		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         srv.URL,
			Version:         "1.0.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())

		hash := sha256.Sum256([]byte(chartData))
		expectedName := fmt.Sprintf("%x.tgz", hash)
		g.Expect(filepath.Base(result.Path)).To(Equal(expectedName))
	})

	t.Run("should support semver constraint versions", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newChartServer(t, "/mychart-1.2.3.tgz")
		result, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         srv.URL,
			Version:         "^1.0.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.Path).To(BeARegularFile())
	})

	t.Run("should error when chart not found in index", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(repoIndexYAML))
		}))
		t.Cleanup(srv.Close)

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "nonexistent",
			RepoURL:         srv.URL,
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("not found"))
	})
}

func TestRepoLocator_Credentials(t *testing.T) {
	t.Parallel()

	t.Run("should forward credentials for same-origin chart URL", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		var receivedAuth string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case indexPath:
				_, _ = w.Write([]byte(repoIndexYAML))
			case "/mychart-1.0.0.tgz":
				receivedAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(chartData))
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         srv.URL,
			Version:         "1.0.0",
			RepositoryCache: t.TempDir(),
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				return &locator.Credentials{Username: "user", Password: "pass"}, nil
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(receivedAuth).ToNot(BeEmpty(), "credentials should be forwarded for same-origin")
	})

	t.Run("should not forward credentials for cross-origin chart URL", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		chartSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.Expect(r.Header.Get("Authorization")).To(BeEmpty(), "credentials should not leak cross-origin")
			_, _ = w.Write([]byte(chartData))
		}))
		t.Cleanup(chartSrv.Close)

		repoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(crossOriginRepoIndex(chartSrv.URL)))
		}))
		t.Cleanup(repoSrv.Close)

		_, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "mychart",
			RepoURL:         repoSrv.URL,
			Version:         "1.0.0",
			RepositoryCache: t.TempDir(),
			Credentials: func(_ context.Context) (*locator.Credentials, error) {
				return &locator.Credentials{Username: "user", Password: "pass"}, nil
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
	})
}

func TestRepoLocator_EmptyRepoURL(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	_, err := locator.Locate(t.Context(), &locator.Request{
		Name:            "mychart",
		RepoURL:         "",
		Version:         "1.0.0",
		RepositoryCache: t.TempDir(),
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, locator.ErrEmptyRepoURL)).To(BeTrue())
}

func TestRepoLocator_EmptyCacheDir(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	repo := &locator.Repo{
		Name:     "mychart",
		RepoURL:  "https://example.com",
		CacheDir: "",
	}

	_, err := repo.Locate(t.Context())
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, locator.ErrEmptyCacheDir)).To(BeTrue())
}

func TestOCILocator_EmptyCacheDir(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oci := &locator.OCI{
		Ref:      "oci://registry.example.com/charts/nginx",
		CacheDir: "",
	}

	_, err := oci.Locate(t.Context())
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, locator.ErrEmptyCacheDir)).To(BeTrue())
}

func TestRepoLocator_PathTraversal(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	const traversalIndexYAML = `apiVersion: v1
entries:
  mychart:
    - version: "1.0.0"
      urls:
        - ../../../etc/mychart-1.0.0.tgz
`

	var requestedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case indexPath:
			_, _ = w.Write([]byte(traversalIndexYAML))
		default:
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(chartData))
		}
	}))
	t.Cleanup(srv.Close)

	_, err := locator.Locate(t.Context(), &locator.Request{
		Name:            "mychart",
		RepoURL:         srv.URL,
		Version:         "1.0.0",
		RepositoryCache: t.TempDir(),
	})

	// url.ResolveReference collapses "../" segments so the request stays
	// under the server's origin rather than escaping.
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(requestedPath).To(Equal("/etc/mychart-1.0.0.tgz"))
}

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
