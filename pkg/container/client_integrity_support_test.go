package container_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Integrity-test registry helpers. These servers serve bytes that diverge
// from the digests and sizes their descriptors and response headers
// advertise, so that client-side verification can be exercised against a
// hostile registry.

// integrityRegistry is a mock OCI registry whose served bytes may diverge
// from the content its descriptors advertise.
type integrityRegistry struct {
	*httptest.Server

	ref            string
	manifestDigest digest.Digest
	chartDigest    digest.Digest
}

// integrityOverrides rewrites the bytes served for one artifact while every
// descriptor and response header keeps advertising the original content.
// Zero values keep the corresponding artifact honest.
type integrityOverrides struct {
	// manifest rewrites the bytes served for the chart manifest.
	manifest func(original []byte) []byte

	// index rewrites the bytes served for the image index.
	index func(original []byte) []byte

	// chart rewrites the bytes served for the chart blob.
	chart func(original []byte) []byte

	// layer overrides the chart layer descriptor recorded in the chart
	// manifest, e.g. with an invalid digest or size.
	layer *ocispec.Descriptor

	// manifestSize overrides the size advertised for the chart manifest in
	// resolution responses; zero advertises the honest size.
	manifestSize int64
}

// chartArtifacts is a self-consistent set of OCI artifacts for one chart:
// its manifest, the config blob, and the chart blob. The bytes actually
// served may diverge from these descriptors.
type chartArtifacts struct {
	manifest       ocispec.Manifest
	manifestBytes  []byte
	manifestDigest digest.Digest

	chartContent []byte
	chartDigest  digest.Digest

	configContent []byte
	configDigest  digest.Digest
}

// newChartArtifacts builds a chart manifest referencing the given chart
// blob. layer, when set, replaces the chart layer descriptor so the manifest
// can advertise a hostile digest or size; the manifest itself stays
// consistent with the bytes it is served as.
func newChartArtifacts(t *testing.T, chartContent []byte, layer *ocispec.Descriptor) chartArtifacts {
	t.Helper()

	artifacts := chartArtifacts{
		chartContent:  chartContent,
		chartDigest:   digest.FromBytes(chartContent),
		configContent: []byte(`{"name":"test-chart","version":"1.0.0"}`),
	}
	artifacts.configDigest = digest.FromBytes(artifacts.configContent)

	chartLayer := ocispec.Descriptor{
		MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		Digest:    artifacts.chartDigest,
		Size:      int64(len(chartContent)),
	}
	if layer != nil {
		chartLayer = *layer
	}

	artifacts.manifest = ocispec.Manifest{
		MediaType:     ocispec.MediaTypeImageManifest,
		SchemaVersion: 2,
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.cncf.helm.config.v1+json",
			Digest:    artifacts.configDigest,
			Size:      int64(len(artifacts.configContent)),
		},
		Layers: []ocispec.Descriptor{chartLayer},
	}

	var err error

	artifacts.manifestBytes, err = json.Marshal(artifacts.manifest)
	if err != nil {
		t.Fatal(err)
	}

	artifacts.manifestDigest = digest.FromBytes(artifacts.manifestBytes)

	return artifacts
}

// newDirectIntegrityRegistry serves a single chart manifest for every
// manifest reference, plus its blobs. Descriptors and headers advertise the
// untampered content; the served bytes follow the overrides.
func newDirectIntegrityRegistry(t *testing.T, chartContent []byte, overrides integrityOverrides) *integrityRegistry {
	t.Helper()

	artifacts := newChartArtifacts(t, chartContent, overrides.layer)

	servedManifest := applyTamper(overrides.manifest, artifacts.manifestBytes)
	servedChart := applyTamper(overrides.chart, artifacts.chartContent)

	manifestSize := int64(len(artifacts.manifestBytes))
	if overrides.manifestSize != 0 {
		manifestSize = overrides.manifestSize
	}

	const repo = "test/chart"

	blobs := map[digest.Digest][]byte{
		artifacts.chartDigest:  servedChart,
		artifacts.configDigest: artifacts.configContent,
	}

	mux := serveChartBlobs(repo, blobs)

	mux.HandleFunc(fmt.Sprintf("/v2/%s/manifests/", repo), func(w http.ResponseWriter, r *http.Request) {
		writeOCIContent(w, r, ocispec.MediaTypeImageManifest, artifacts.manifestDigest, manifestSize, servedManifest)
	})

	return newIntegrityRegistry(t, mux, artifacts, artifacts.manifestDigest)
}

// newIndexIntegrityRegistry serves an image index with a single chart
// manifest candidate. Digest references select the candidate manifest; every
// other reference resolves to the index.
func newIndexIntegrityRegistry(t *testing.T, chartContent []byte, overrides integrityOverrides) *integrityRegistry {
	t.Helper()

	artifacts := newChartArtifacts(t, chartContent, overrides.layer)

	servedChild := applyTamper(overrides.manifest, artifacts.manifestBytes)

	idx := ocispec.Index{
		MediaType:     ocispec.MediaTypeImageIndex,
		SchemaVersion: 2,
		Manifests: []ocispec.Descriptor{{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    artifacts.manifestDigest,
			Size:      int64(len(artifacts.manifestBytes)),
		}},
	}

	indexBytes, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}

	indexDigest := digest.FromBytes(indexBytes)
	servedIndex := applyTamper(overrides.index, indexBytes)

	const repo = "test/chart"
	manifestPrefix := fmt.Sprintf("/v2/%s/manifests/", repo)

	blobs := map[digest.Digest][]byte{
		artifacts.chartDigest:  applyTamper(overrides.chart, artifacts.chartContent),
		artifacts.configDigest: artifacts.configContent,
	}

	mux := serveChartBlobs(repo, blobs)

	mux.HandleFunc(manifestPrefix, func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, manifestPrefix)

		if ref == artifacts.manifestDigest.String() {
			writeOCIContent(
				w, r,
				ocispec.MediaTypeImageManifest,
				artifacts.manifestDigest,
				int64(len(artifacts.manifestBytes)),
				servedChild,
			)

			return
		}

		writeOCIContent(w, r, ocispec.MediaTypeImageIndex, indexDigest, int64(len(indexBytes)), servedIndex)
	})

	return newIntegrityRegistry(t, mux, artifacts, indexDigest)
}

// serveChartBlobs registers a blob endpoint serving the given content. The
// Docker-Content-Digest header always claims the requested digest.
func serveChartBlobs(repo string, blobs map[digest.Digest][]byte) *http.ServeMux {
	mux := http.NewServeMux()

	blobPrefix := fmt.Sprintf("/v2/%s/blobs/", repo)

	mux.HandleFunc(blobPrefix, func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, blobPrefix)

		d, err := digest.Parse(ref)
		if err != nil {
			http.Error(w, "bad digest", http.StatusBadRequest)

			return
		}

		content, ok := blobs[d]
		if !ok {
			http.NotFound(w, r)

			return
		}

		writeOCIContent(w, r, "application/octet-stream", d, int64(len(content)), content)
	})

	return mux
}

// writeOCIContent serves one registry artifact. Every response advertises
// the original digest and size while the body carries the served bytes.
// GET bodies are flushed as chunked responses that declare no length, so
// that transport-level length checks cannot mask the client's own
// verification of truncated or extended bodies.
func writeOCIContent(
	w http.ResponseWriter,
	r *http.Request,
	contentType string,
	dgst digest.Digest,
	size int64,
	content []byte,
) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", dgst.String())

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(int(size)))

		return
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	_, _ = w.Write(content)
}

// newIntegrityRegistry starts the server for the given mux and returns the
// registry view of the artifacts it serves. rootDigest is the digest a
// digest-pinned pull should request.
func newIntegrityRegistry(
	t *testing.T,
	mux *http.ServeMux,
	artifacts chartArtifacts,
	rootDigest digest.Digest,
) *integrityRegistry {
	t.Helper()

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const repo = "test/chart"

	return &integrityRegistry{
		Server:         srv,
		ref:            srv.Listener.Addr().String() + "/" + repo,
		manifestDigest: rootDigest,
		chartDigest:    artifacts.chartDigest,
	}
}

// newResolvedAsRegistry answers every manifest reference with the given
// artifacts' manifest, advertising that manifest's digest in its headers.
// The returned manifestDigest is an unrelated valid digest that a pinned
// pull can request.
func newResolvedAsRegistry(t *testing.T, artifacts chartArtifacts) *integrityRegistry {
	t.Helper()

	const repo = "test/chart"

	blobs := map[digest.Digest][]byte{
		artifacts.chartDigest:  artifacts.chartContent,
		artifacts.configDigest: artifacts.configContent,
	}

	mux := serveChartBlobs(repo, blobs)

	mux.HandleFunc(fmt.Sprintf("/v2/%s/manifests/", repo), func(w http.ResponseWriter, r *http.Request) {
		writeOCIContent(
			w, r,
			ocispec.MediaTypeImageManifest,
			artifacts.manifestDigest,
			int64(len(artifacts.manifestBytes)),
			artifacts.manifestBytes,
		)
	})

	return newIntegrityRegistry(t, mux, artifacts, digest.FromString("digest pinned by the caller"))
}

// layerDescriptor builds a chart layer descriptor with the given digest and
// size, keeping the recognised chart media type.
func layerDescriptor(dgst string, size int64) *ocispec.Descriptor {
	return &ocispec.Descriptor{
		MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		Digest:    digest.Digest(dgst),
		Size:      size,
	}
}

// applyTamper returns tamper(original), or original when tamper is nil.
func applyTamper(tamper func([]byte) []byte, original []byte) []byte {
	if tamper == nil {
		return original
	}

	return tamper(original)
}

// tamperJSON returns document with its schemaVersion value rewritten from 2
// to 9. The substitution is same-length and keeps the document syntactically
// valid, so only byte-level digest verification can detect it.
func tamperJSON(t *testing.T, document []byte) []byte {
	t.Helper()

	const (
		from = `"schemaVersion":2`
		to   = `"schemaVersion":9`
	)

	tampered := bytes.Replace(document, []byte(from), []byte(to), 1)
	if bytes.Equal(tampered, document) {
		t.Fatalf("tamper substitution %q not found in document", from)
	}

	return tampered
}

// flipByte returns content with the byte at index i inverted. The content
// length is preserved so only digest verification can detect the change.
func flipByte(content []byte, i int) []byte {
	tampered := bytes.Clone(content)
	tampered[i] ^= 0xff

	return tampered
}
