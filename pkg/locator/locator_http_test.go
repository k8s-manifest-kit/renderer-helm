package locator_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k8s-manifest-kit/pkg/util/httpclient"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

func TestRepoLocator_DefaultClientUsesFreshConnections(t *testing.T) {
	g := NewWithT(t)

	var (
		mu    sync.Mutex
		paths []string
		reuse []bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		switch r.URL.Path {
		case indexPath:
			_, _ = w.Write([]byte(repoIndexYAML))
		case "/mychart-1.2.3.tgz":
			_, _ = w.Write([]byte(chartData))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			defer mu.Unlock()
			reuse = append(reuse, info.Reused)
		},
	}
	repo := &locator.Repo{
		Name:     "mychart",
		RepoURL:  srv.URL,
		Version:  "1.2.3",
		CacheDir: t.TempDir(),
	}
	result, err := repo.Locate(httptrace.WithClientTrace(t.Context(), trace))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.Path).To(BeARegularFile())

	mu.Lock()
	observedPaths := strings.Join(paths, "\n")
	observedReuse := append([]bool(nil), reuse...)
	mu.Unlock()
	g.Expect(observedPaths).To(ContainSubstring(indexPath))
	g.Expect(observedPaths).To(ContainSubstring("/mychart-1.2.3.tgz"))
	g.Expect(observedReuse).To(HaveLen(2))
	for _, reused := range observedReuse {
		g.Expect(reused).To(BeFalse(), "repository downloads must not reuse connections")
	}
}

func TestRepoLocator_InjectedClientOwnershipAndBodyClosure(t *testing.T) {
	tests := []struct {
		name               string
		status             int
		readErr            error
		redirectIndex      bool
		wantError          string
		wantBodyCloseCount int32
		wantRedirectClose  int32
		wantRedirectCalls  int32
	}{
		{
			name:               "success",
			wantBodyCloseCount: 2,
		},
		{
			name:               "same-origin redirect",
			redirectIndex:      true,
			wantBodyCloseCount: 2,
			wantRedirectClose:  1,
			wantRedirectCalls:  1,
		},
		{
			name:               "status error",
			status:             http.StatusServiceUnavailable,
			wantError:          locator.ErrUnexpectedStatus.Error(),
			wantBodyCloseCount: 1,
		},
		{
			name:               "read error",
			readErr:            errors.New("synthetic response read failure"),
			wantError:          "synthetic response read failure",
			wantBodyCloseCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			var bodyCloseCount atomic.Int32
			var redirectBodyCloseCount atomic.Int32
			var redirectCalls atomic.Int32
			transport := &functionRoundTripper{}
			transport.roundTrip = func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case indexPath:
					if tt.status != 0 {
						return testHTTPResponse(req, tt.status, newTestBody("unavailable", &bodyCloseCount, nil)), nil
					}
					if tt.readErr != nil {
						resp := testHTTPResponse(req, http.StatusOK, newTestBody("", &bodyCloseCount, tt.readErr))

						return resp, nil //nolint:nilerr // The synthetic error is returned by the response body.
					}
					if tt.redirectIndex {
						resp := testHTTPResponse(req, http.StatusFound, newTestBody("redirect", &redirectBodyCloseCount, nil))
						resp.Header.Set("Location", "/redirected-index.yaml")

						return resp, nil
					}

					return testHTTPResponse(req, http.StatusOK, newTestBody(repoIndexYAML, &bodyCloseCount, nil)), nil
				case "/redirected-index.yaml":
					return testHTTPResponse(req, http.StatusOK, newTestBody(repoIndexYAML, &bodyCloseCount, nil)), nil
				case "/mychart-1.2.3.tgz":
					return testHTTPResponse(req, http.StatusOK, newTestBody(chartData, &bodyCloseCount, nil)), nil
				default:
					return nil, errors.New("unexpected test request path: " + req.URL.Path)
				}
			}
			callerClient := &http.Client{
				Transport: transport,
				Timeout:   7 * time.Second,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					redirectCalls.Add(1)

					return nil
				},
			}
			repo := &locator.Repo{
				Name:       "mychart",
				RepoURL:    "http://repo.example.test",
				Version:    "1.2.3",
				CacheDir:   t.TempDir(),
				HTTPClient: callerClient,
			}
			_, err := repo.Locate(t.Context())
			if tt.wantError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantError))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}

			g.Expect(callerClient.Transport).To(BeIdenticalTo(transport))
			g.Expect(callerClient.Timeout).To(Equal(7 * time.Second))
			g.Expect(callerClient.CheckRedirect).ToNot(BeNil())
			g.Expect(transport.closeIdleCalls.Load()).To(BeZero())
			g.Expect(bodyCloseCount.Load()).To(Equal(tt.wantBodyCloseCount))
			g.Expect(redirectBodyCloseCount.Load()).To(Equal(tt.wantRedirectClose))
			g.Expect(redirectCalls.Load()).To(Equal(tt.wantRedirectCalls))
		})
	}
}

func TestRepoLocator_InjectedClientWithNilTransportRemainsUnchanged(t *testing.T) {
	g := NewWithT(t)

	srv := newChartServer(t, "/mychart-1.2.3.tgz")
	callerClient := &http.Client{Timeout: 3 * time.Second}
	repo := &locator.Repo{
		Name:       "mychart",
		RepoURL:    srv.URL,
		Version:    "1.2.3",
		CacheDir:   t.TempDir(),
		HTTPClient: callerClient,
	}
	_, err := repo.Locate(t.Context())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(callerClient.Transport).To(BeNil())
	g.Expect(callerClient.Timeout).To(Equal(3 * time.Second))
}

func TestRepoLocator_CancellationDuringIndexAndChartRequests(t *testing.T) {
	for _, blockedPath := range []string{indexPath, "/mychart-1.2.3.tgz"} {
		t.Run(blockedPath, func(t *testing.T) {
			g := NewWithT(t)
			started := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == blockedPath {
					started <- struct{}{}
					<-r.Context().Done()

					return
				}
				if r.URL.Path == indexPath {
					_, _ = w.Write([]byte(repoIndexYAML))

					return
				}
				http.NotFound(w, r)
			}))
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			repo := &locator.Repo{
				Name:     "mychart",
				RepoURL:  srv.URL,
				Version:  "1.2.3",
				CacheDir: t.TempDir(),
			}
			result := make(chan error, 1)
			go func() {
				_, err := repo.Locate(ctx)
				result <- err
			}()

			select {
			case <-started:
				cancel()
			case <-time.After(5 * time.Second):
				t.Fatal("repository request did not reach the cancellation barrier")
			}

			err := <-result
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(context.Canceled.Error()))
		})
	}
}

func TestOCILocator_ForwardsCallerHTTPClient(t *testing.T) {
	g := NewWithT(t)

	chartContent := []byte("injected locator chart")
	srv := newMockOCIRegistry(t, chartContent)
	transport := httpclient.NewTransport()
	observedTransport := &functionRoundTripper{roundTrip: transport.RoundTrip}
	callerClient := &http.Client{Transport: observedTransport, Timeout: 11 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)

	oci := &locator.OCI{
		Ref:        "oci://" + srv.ref,
		Version:    "1.0.0",
		CacheDir:   t.TempDir(),
		PlainHTTP:  true,
		HTTPClient: callerClient,
	}
	result, err := oci.Locate(t.Context())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.Path).To(BeARegularFile())
	g.Expect(observedTransport.requests.Load()).To(BeNumerically(">", 0))
	g.Expect(observedTransport.closeIdleCalls.Load()).To(BeZero())
	g.Expect(callerClient.Transport).To(BeIdenticalTo(observedTransport))
	g.Expect(callerClient.Timeout).To(Equal(11 * time.Second))
}

type functionRoundTripper struct {
	roundTrip      func(*http.Request) (*http.Response, error)
	requests       atomic.Int32
	closeIdleCalls atomic.Int32
}

func (t *functionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests.Add(1)

	resp, err := t.roundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("test round trip: %w", err)
	}

	return resp, nil
}

func (t *functionRoundTripper) CloseIdleConnections() {
	t.closeIdleCalls.Add(1)
}

type testBody struct {
	data      []byte
	offset    int
	closeCall *atomic.Int32
	readErr   error
}

func newTestBody(value string, closeCall *atomic.Int32, readErr error) *testBody {
	return &testBody{data: []byte(value), closeCall: closeCall, readErr: readErr}
}

func (b *testBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}

	if b.offset == len(b.data) {
		return 0, io.EOF
	}

	n := copy(p, b.data[b.offset:])
	b.offset += n

	return n, nil
}

func (b *testBody) Close() error {
	b.closeCall.Add(1)

	return nil
}

func testHTTPResponse(req *http.Request, status int, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        make(http.Header),
		Body:          body,
		ContentLength: -1,
		Request:       req,
	}
}
