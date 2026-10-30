package container_test

import (
	"errors"
	"io"
	"testing"

	"oras.land/oras-go/v2/content"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/container"

	. "github.com/onsi/gomega"
)

// The registry responses in these tests advertise plausible, matching
// Docker-Content-Digest headers while serving bytes that diverge from the
// advertised content, so only client-side byte verification can reject
// them. Tampered JSON documents stay syntactically valid for the same
// reason: a regression to parse-only checking would not catch them.

func TestClient_Pull_Integrity(t *testing.T) {
	t.Parallel()

	t.Run("should reject a root manifest with substituted bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			manifest: func(original []byte) []byte {
				return tamperJSON(t, original)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(data).To(BeNil())
		g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())
		g.Expect(errors.Is(err, content.ErrMismatchedDigest)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.manifestDigest.String()))
	})

	t.Run("should reject a chart blob with substituted bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			chart: func(original []byte) []byte {
				return flipByte(original, 0)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(data).To(BeNil())
		g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.chartDigest.String()))
	})

	t.Run("should reject a truncated root manifest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			manifest: func(original []byte) []byte {
				return original[:len(original)/2]
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, io.ErrUnexpectedEOF)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.manifestDigest.String()))
	})

	t.Run("should reject a root manifest with trailing bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			manifest: func(original []byte) []byte {
				return append(original, "trailing"...)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, content.ErrTrailingData)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.manifestDigest.String()))
	})

	t.Run("should reject a truncated chart blob", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			chart: func(original []byte) []byte {
				return original[:len(original)/2]
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, io.ErrUnexpectedEOF)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.chartDigest.String()))
	})

	t.Run("should reject a chart blob with trailing bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			chart: func(original []byte) []byte {
				return append(original, "trailing"...)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, content.ErrTrailingData)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.chartDigest.String()))
	})
}

func TestClient_Pull_DescriptorIntegrity(t *testing.T) {
	t.Parallel()

	t.Run("should reject an oversized root manifest descriptor", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			manifestSize: 16 << 20,
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrManifestTooLarge)).To(BeTrue())
	})

	t.Run("should reject an invalid chart layer digest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			layer: layerDescriptor("sha256:not-hex", 13),
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrInvalidDigest)).To(BeTrue())
	})

	t.Run("should reject an unsupported chart layer digest algorithm", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			layer: layerDescriptor("md5:0123456789abcdef0123456789abcdef", 13),
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrInvalidDigest)).To(BeTrue())
	})

	t.Run("should reject a zero-size chart layer descriptor", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			layer: layerDescriptor("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 0),
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrInvalidDescriptorSize)).To(BeTrue())
	})

	t.Run("should reject a negative-size chart layer descriptor", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			layer: layerDescriptor("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", -1),
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrInvalidDescriptorSize)).To(BeTrue())
	})

	t.Run("should reject an oversized chart layer descriptor", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newDirectIntegrityRegistry(t, []byte("chart-content"), integrityOverrides{
			layer: layerDescriptor("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 300<<20),
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrBlobTooLarge)).To(BeTrue())
	})
}

func TestClient_PullIndex_Integrity(t *testing.T) {
	t.Parallel()

	t.Run("should reject an index with substituted bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newIndexIntegrityRegistry(t, []byte("index-chart-content"), integrityOverrides{
			index: func(original []byte) []byte {
				return tamperJSON(t, original)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		_, err = client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("unable to fetch index"))
	})

	t.Run("should reject a sole index candidate with substituted bytes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		srv := newIndexIntegrityRegistry(t, []byte("index-chart-content"), integrityOverrides{
			manifest: func(original []byte) []byte {
				return tamperJSON(t, original)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.Pull(t.Context(), "1.0.0")
		g.Expect(err).To(HaveOccurred())
		g.Expect(data).To(BeNil())
		g.Expect(err.Error()).To(ContainSubstring("all index candidates failed"))
		g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())
	})
}

func TestClient_PullDigest_Integrity(t *testing.T) {
	t.Parallel()

	t.Run("should reject a different manifest served for a pinned digest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// The registry answers the pinned digest with a different manifest
		// of the same size while the headers keep claiming the requested
		// digest, so only body verification can detect the substitution.
		srv := newDirectIntegrityRegistry(t, []byte("pinned-chart-content"), integrityOverrides{
			manifest: func(original []byte) []byte {
				return tamperJSON(t, original)
			},
		})

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.PullDigest(t.Context(), srv.manifestDigest)
		g.Expect(err).To(HaveOccurred())
		g.Expect(data).To(BeNil())
		g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring(srv.manifestDigest.String()))
	})

	t.Run("should reject a pinned reference resolved to a different digest", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// The registry answers the pinned digest with a wholly different
		// chart and advertises that chart's digest in its headers.
		otherChart := []byte("entirely-different-chart")

		other := newChartArtifacts(t, otherChart, nil)

		srv := newResolvedAsRegistry(t, other)

		client, err := container.NewClient(srv.ref, container.WithPlainHTTP(true))
		g.Expect(err).ToNot(HaveOccurred())

		data, err := client.PullDigest(t.Context(), srv.manifestDigest)
		g.Expect(err).To(HaveOccurred())
		g.Expect(data).To(BeNil())
		g.Expect(err.Error()).To(ContainSubstring("unable to resolve"))
	})
}
