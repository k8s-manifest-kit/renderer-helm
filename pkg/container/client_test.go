package container_test

import (
	"errors"
	"testing"

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
