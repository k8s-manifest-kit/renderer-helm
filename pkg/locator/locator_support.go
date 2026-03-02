package locator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"sigs.k8s.io/yaml"
)

const (
	httpTimeout         = 60 * time.Second
	dialTimeout         = 10 * time.Second
	tlsHandshakeTimeout = 10 * time.Second
	responseTimeout     = 30 * time.Second
)

var (
	// ErrUnexpectedStatus is returned when an HTTP request receives a non-200 status code.
	ErrUnexpectedStatus = errors.New("unexpected HTTP status")

	// ErrNoChartYAML is returned when a chart archive does not contain a Chart.yaml.
	ErrNoChartYAML = errors.New("archive does not contain a Chart.yaml")

	// ErrNoValidSemverTag is returned when none of the registry tags are valid semver.
	ErrNoValidSemverTag = errors.New("no valid semver tag found")
)

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ResponseHeaderTimeout: responseTimeout,
		},
	}
}

func urlsShareOrigin(u1 *url.URL, u2 *url.URL) bool {
	return u1.Scheme == u2.Scheme &&
		u1.Hostname() == u2.Hostname() &&
		effectivePort(u1) == effectivePort(u2)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}

	switch u.Scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func httpGet(ctx context.Context, rawURL string, creds *Credentials) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create HTTP request: %w", err)
	}

	if creds.hasAuth() {
		req.SetBasicAuth(creds.Username, creds.Password)
	}

	resp, err := newHTTPClient().Do(req) //nolint:gosec // URL is constructed from user-provided repo config
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d from %s", ErrUnexpectedStatus, resp.StatusCode, rawURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read HTTP response body: %w", err)
	}

	return data, nil
}

// ExtractChartMeta opens a .tgz chart archive and returns the metadata from
// the top-level Chart.yaml (i.e. <chartname>/Chart.yaml, not a dependency).
func ExtractChartMeta(path string) (chart.Metadata, error) {
	f, err := os.Open(path) //nolint:gosec // caller controls path
	if err != nil {
		return chart.Metadata{}, fmt.Errorf("unable to open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return chart.Metadata{}, fmt.Errorf("unable to create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	var best string
	var bestData []byte

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return chart.Metadata{}, fmt.Errorf("unable to read tar entry: %w", err)
		}

		name := filepath.ToSlash(hdr.Name)
		if !strings.HasSuffix(name, "/Chart.yaml") && name != "Chart.yaml" {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return chart.Metadata{}, fmt.Errorf("unable to read %q from archive: %w", name, err)
		}

		if best == "" || len(name) < len(best) {
			best = name
			bestData = data
		}
	}

	if best == "" {
		return chart.Metadata{}, fmt.Errorf("%w: %s", ErrNoChartYAML, path)
	}

	var meta chart.Metadata
	if err := yaml.Unmarshal(bestData, &meta); err != nil {
		return chart.Metadata{}, fmt.Errorf("unable to parse Chart.yaml: %w", err)
	}

	return meta, nil
}

// latestSemver parses all valid semver tags, sorts them descending,
// and returns the highest. Non-semver tags are silently skipped.
func latestSemver(tags []string) (*semver.Version, error) {
	versions := make([]*semver.Version, 0, len(tags))

	for _, t := range tags {
		v, err := semver.NewVersion(t)
		if err != nil {
			continue
		}

		versions = append(versions, v)
	}

	if len(versions) == 0 {
		return nil, ErrNoValidSemverTag
	}

	sort.Sort(sort.Reverse(semver.Collection(versions)))

	return versions[0], nil
}
