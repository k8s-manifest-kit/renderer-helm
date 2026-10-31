package locator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

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
