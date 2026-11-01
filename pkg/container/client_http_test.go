package container_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/k8s-manifest-kit/pkg/util/httpclient"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/container"

	. "github.com/onsi/gomega"
)

func TestClient_DefaultHTTPClientDoesNotReuseConnections(t *testing.T) {
	g := NewWithT(t)

	const repository = "test/chart"
	chartContent := []byte("chart archive bytes")
	artifacts := newChartArtifacts(t, chartContent, nil)
	blobs := map[digest.Digest][]byte{
		artifacts.configDigest: artifacts.configContent,
		artifacts.chartDigest:  artifacts.chartContent,
	}

	var (
		mu    sync.Mutex
		paths []string
	)
	recordRequest := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, fmt.Sprintf("%s %s", r.Method, r.URL.Path))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/", func(w http.ResponseWriter, r *http.Request) {
		recordRequest(r)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v2/"+repository+"/manifests/{reference}", func(w http.ResponseWriter, r *http.Request) {
		recordRequest(r)
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", artifacts.manifestDigest.String())
		w.Header().Set("Content-Length", strconv.Itoa(len(artifacts.manifestBytes)))
		_, _ = w.Write(artifacts.manifestBytes)
	})
	mux.HandleFunc("GET /v2/"+repository+"/blobs/{digest}", func(w http.ResponseWriter, r *http.Request) {
		recordRequest(r)
		blobDigest, err := digest.Parse(r.PathValue("digest"))
		if err != nil {
			http.Error(w, "invalid digest", http.StatusBadRequest)

			return
		}

		data, ok := blobs[blobDigest]
		if !ok {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Docker-Content-Digest", blobDigest.String())
		_, _ = w.Write(data)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var (
		traceMu sync.Mutex
		reuse   []bool
	)
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			traceMu.Lock()
			defer traceMu.Unlock()
			reuse = append(reuse, info.Reused)
		},
	}
	ctx := httptrace.WithClientTrace(t.Context(), trace)

	client, err := container.NewClient(
		strings.TrimPrefix(srv.URL, "http://")+"/"+repository,
		container.WithPlainHTTP(true),
	)
	g.Expect(err).ToNot(HaveOccurred())

	data, err := client.Pull(ctx, "1.0.0")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(data).To(Equal(chartContent))

	mu.Lock()
	observedPaths := strings.Join(paths, "\n")
	mu.Unlock()
	g.Expect(observedPaths).To(ContainSubstring("/v2/" + repository + "/manifests/1.0.0"))
	g.Expect(observedPaths).To(ContainSubstring("/v2/" + repository + "/blobs/" + artifacts.chartDigest.String()))

	traceMu.Lock()
	observedReuse := append([]bool(nil), reuse...)
	traceMu.Unlock()
	g.Expect(observedReuse).To(
		HaveLen(3),
		"registry ping, manifest resolution, and chart blob should reach the client transport",
	)
	for _, reused := range observedReuse {
		g.Expect(reused).To(BeFalse(), "the default registry client must not reuse connections; trace=%v", observedReuse)
	}
}

func TestClient_WithHTTPClientPreservesCallerConfiguration(t *testing.T) {
	g := NewWithT(t)

	chartContent := []byte("injected chart archive bytes")
	artifacts := newChartArtifacts(t, chartContent, nil)
	blobs := map[digest.Digest][]byte{
		artifacts.configDigest: artifacts.configContent,
		artifacts.chartDigest:  artifacts.chartContent,
	}
	const repository = "test/chart"
	const manifestPath = "/v2/" + repository + "/manifests/"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v2/"+repository+"/manifests/{reference}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("reference") == "1.0.0" {
			http.Redirect(w, r, manifestPath+"redirected", http.StatusFound)

			return
		}

		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", artifacts.manifestDigest.String())
		w.Header().Set("Content-Length", strconv.Itoa(len(artifacts.manifestBytes)))
		_, _ = w.Write(artifacts.manifestBytes)
	})
	mux.HandleFunc("GET /v2/"+repository+"/blobs/{digest}", func(w http.ResponseWriter, r *http.Request) {
		blobDigest, err := digest.Parse(r.PathValue("digest"))
		if err != nil {
			http.Error(w, "invalid digest", http.StatusBadRequest)

			return
		}

		data, ok := blobs[blobDigest]
		if !ok {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Docker-Content-Digest", blobDigest.String())
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	transport := httpclient.NewTransport()
	observedTransport := &observedRoundTripper{next: transport}
	var redirectCalls atomic.Int32
	checkRedirect := func(_ *http.Request, _ []*http.Request) error {
		redirectCalls.Add(1)

		return nil
	}
	callerClient := &http.Client{
		Transport:     observedTransport,
		Timeout:       9 * time.Second,
		CheckRedirect: checkRedirect,
	}
	t.Cleanup(transport.CloseIdleConnections)
	var callerOptions container.ClientOptions
	container.WithHTTPClient(callerClient).ApplyTo(&callerOptions)
	(container.ClientOptions{}).ApplyTo(&callerOptions)

	client, err := container.NewClient(
		strings.TrimPrefix(srv.URL, "http://")+"/"+repository,
		container.WithPlainHTTP(true),
		container.WithHTTPClient(nil),
		callerOptions,
	)
	g.Expect(err).ToNot(HaveOccurred())

	data, err := client.Pull(t.Context(), "1.0.0")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(data).To(Equal(chartContent))
	g.Expect(redirectCalls.Load()).To(BeNumerically(">", 0))
	g.Expect(callerClient.Transport).To(BeIdenticalTo(observedTransport))
	g.Expect(callerClient.Timeout).To(Equal(9 * time.Second))
	g.Expect(callerClient.CheckRedirect).ToNot(BeNil())

	snapshot := observedTransport.snapshot()
	g.Expect(snapshot.requests).To(BeNumerically(">", 0))
	g.Expect(snapshot.closeIdleCalls).To(BeZero(), "the registry client must not close a caller-owned transport")
}

func TestClient_WithHTTPClientNilTransportRemainsCallerOwned(t *testing.T) {
	g := NewWithT(t)

	chartContent := []byte("caller default transport chart")
	srv := newMockOCIRegistry(t, chartContent)
	callerClient := &http.Client{}

	client, err := container.NewClient(
		srv.ref,
		container.WithPlainHTTP(true),
		container.WithHTTPClient(callerClient),
	)
	g.Expect(err).ToNot(HaveOccurred())

	data, err := client.Pull(t.Context(), "1.0.0")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(data).To(Equal(chartContent))
	g.Expect(callerClient.Transport).To(BeNil())
}

func TestClient_WithHTTPClientNilOverridesEarlierOption(t *testing.T) {
	g := NewWithT(t)

	chartContent := []byte("default client after nil option")
	srv := newMockOCIRegistry(t, chartContent)
	transport := httpclient.NewTransport()
	observedTransport := &observedRoundTripper{next: transport}
	callerClient := &http.Client{Transport: observedTransport}
	t.Cleanup(transport.CloseIdleConnections)

	client, err := container.NewClient(
		srv.ref,
		container.WithPlainHTTP(true),
		container.WithHTTPClient(callerClient),
		container.WithHTTPClient(nil),
	)
	g.Expect(err).ToNot(HaveOccurred())
	data, err := client.Pull(t.Context(), "1.0.0")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(data).To(Equal(chartContent))
	snapshot := observedTransport.snapshot()
	g.Expect(snapshot.requests).To(BeZero())
	g.Expect(snapshot.closeIdleCalls).To(BeZero())
}

func TestClientOptionsApplyToPreservesUnsetValues(t *testing.T) {
	g := NewWithT(t)

	target := container.ClientOptions{PlainHTTP: true}
	(container.ClientOptions{}).ApplyTo(&target)
	(container.ClientOptions{PlainHTTP: false}).ApplyTo(&target)
	g.Expect(target.PlainHTTP).To(BeTrue())

	populated := container.ClientOptions{}
	(container.ClientOptions{PlainHTTP: true}).ApplyTo(&populated)
	g.Expect(populated.PlainHTTP).To(BeTrue())
}

type observedRoundTripper struct {
	mu             sync.Mutex
	next           http.RoundTripper
	requests       int
	closeIdleCalls int
}

type observedRoundTripperSnapshot struct {
	requests       int
	closeIdleCalls int
}

func (t *observedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests++
	t.mu.Unlock()

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("delegate HTTP round trip: %w", err)
	}

	return resp, nil
}

func (t *observedRoundTripper) CloseIdleConnections() {
	t.mu.Lock()
	t.closeIdleCalls++
	t.mu.Unlock()
}

func (t *observedRoundTripper) snapshot() observedRoundTripperSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	return observedRoundTripperSnapshot{
		requests:       t.requests,
		closeIdleCalls: t.closeIdleCalls,
	}
}
