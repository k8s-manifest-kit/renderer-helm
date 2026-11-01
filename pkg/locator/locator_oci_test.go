package locator_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

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

func TestOCILocator_IntegrityFailure(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// A registry serving chart bytes that do not match the advertised digest
	// must fail the pull, and no chart may be cached from unverifiable
	// content.
	chartContent := []byte("locator-integrity-chart")
	tampered := bytes.Clone(chartContent)
	tampered[0] ^= 0xff

	srv := newMockOCIRegistryServingChart(t, chartContent, tampered)

	cacheDir := t.TempDir()

	oci := &locator.OCI{
		Ref:       fmt.Sprintf("oci://%s@%s", srv.ref, srv.manifestDigest),
		CacheDir:  cacheDir,
		PlainHTTP: true,
	}

	_, err := oci.Locate(t.Context())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring("unable to pull chart by digest")))
	g.Expect(errors.Is(err, container.ErrContentMismatch)).To(BeTrue())

	entries, readErr := os.ReadDir(cacheDir)
	g.Expect(readErr).ToNot(HaveOccurred())
	g.Expect(entries).To(BeEmpty())
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
