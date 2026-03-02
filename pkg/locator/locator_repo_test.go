package locator_test

import (
	"testing"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestRepoLocator_Integration(t *testing.T) {
	t.Parallel()

	t.Run("should download chart with explicit version", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		path, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "nginx",
			RepoURL:         "https://charts.bitnami.com/bitnami",
			Version:         "18.1.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(path).To(BeARegularFile())

		meta, err := locator.ExtractChartMeta(path)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta).To(MatchFields(IgnoreExtras, Fields{
			"Name":    Equal("nginx"),
			"Version": Equal("18.1.0"),
		}))
	})

	t.Run("should resolve version from semver constraint", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		path, err := locator.Locate(t.Context(), &locator.Request{
			Name:            "nginx",
			RepoURL:         "https://charts.bitnami.com/bitnami",
			Version:         "~18.1.0",
			RepositoryCache: t.TempDir(),
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(path).To(BeARegularFile())

		meta, err := locator.ExtractChartMeta(path)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(meta).To(MatchFields(IgnoreExtras, Fields{
			"Name":    Equal("nginx"),
			"Version": HavePrefix("18.1."),
		}))
	})
}
