package locator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// ErrNoDownloadURLs is returned when a resolved chart version has no download URLs.
var ErrNoDownloadURLs = errors.New("chart version has no download URLs")

// Repo resolves charts hosted in a classic Helm HTTP/HTTPS repository.
type Repo struct {
	Name        string
	RepoURL     string
	Version     string
	Credentials *Credentials
	CacheDir    string
	HTTPClient  *http.Client
}

// Locate downloads the chart from a Helm repository and returns the local cache path.
func (r *Repo) Locate(ctx context.Context) (Result, error) {
	if err := os.MkdirAll(r.CacheDir, dirPermissions); err != nil {
		return Result{}, fmt.Errorf("unable to create repository cache directory: %w", err)
	}

	chartURL, err := r.resolveChartURL(ctx)
	if err != nil {
		return Result{}, err
	}

	creds := r.downloadCredentials(chartURL)

	data, err := httpGet(ctx, r.HTTPClient, chartURL, creds)
	if err != nil {
		return Result{}, fmt.Errorf("unable to download chart: %w", err)
	}

	hash := sha256.Sum256(data)
	filename := filepath.Join(r.CacheDir, fmt.Sprintf("%x.tgz", hash))

	if err := os.WriteFile(filename, data, filePermissions); err != nil {
		return Result{}, fmt.Errorf("unable to write chart to cache: %w", err)
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		return Result{}, fmt.Errorf("unable to resolve absolute path for %q: %w", filename, err)
	}

	return Result{Path: abs, SourceType: SourceRepo}, nil
}

func (r *Repo) resolveChartURL(ctx context.Context) (string, error) {
	indexURL := strings.TrimSuffix(r.RepoURL, "/") + "/index.yaml"

	data, err := httpGet(ctx, r.HTTPClient, indexURL, r.Credentials)
	if err != nil {
		return "", fmt.Errorf("unable to fetch repository index from %q: %w", indexURL, err)
	}

	var idx repoIndex
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return "", fmt.Errorf("unable to parse repository index: %w", err)
	}

	cv, err := idx.resolve(r.Name, r.Version)
	if err != nil {
		return "", fmt.Errorf("unable to find chart %q in repo %q: %w", r.Name, r.RepoURL, err)
	}

	if len(cv.URLs) == 0 {
		return "", fmt.Errorf("%w: chart %q version %q", ErrNoDownloadURLs, r.Name, cv.Version)
	}

	chartURL := cv.URLs[0]

	if u, err := url.Parse(chartURL); err == nil && !u.IsAbs() {
		chartURL = strings.TrimSuffix(r.RepoURL, "/") + "/" + chartURL
	}

	return chartURL, nil
}

// downloadCredentials returns credentials for the chart download, applying
// same-origin protection to prevent credential leakage across hosts.
func (r *Repo) downloadCredentials(chartURL string) *Credentials {
	if !r.Credentials.hasAuth() {
		return nil
	}

	u1, err := url.Parse(r.RepoURL)
	if err != nil {
		return nil
	}

	u2, err := url.Parse(chartURL)
	if err != nil {
		return nil
	}

	if !urlsShareOrigin(u1, u2) {
		return nil
	}

	return r.Credentials
}
