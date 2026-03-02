package locator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/registry"
)

// ErrEmptyChartData is returned when an OCI pull returns no chart content.
var ErrEmptyChartData = errors.New("OCI pull returned empty chart data")

// ErrNoTags is returned when a registry has no tags for the given reference.
var ErrNoTags = errors.New("no tags found")

// OCI resolves charts stored in an OCI-compatible registry.
type OCI struct {
	Ref         string
	Version     string
	Credentials *Credentials
	CacheDir    string
}

// Locate pulls the chart from an OCI registry and returns the local cache path.
func (o *OCI) Locate(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled before OCI locate: %w", err)
	}

	if err := os.MkdirAll(o.CacheDir, dirPermissions); err != nil {
		return "", fmt.Errorf("unable to create repository cache directory: %w", err)
	}

	data, err := o.pull(ctx)
	if err != nil {
		return "", err
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled before writing OCI chart to cache: %w", err)
	}

	hash := sha256.Sum256(data)
	filename := filepath.Join(o.CacheDir, fmt.Sprintf("%x.tgz", hash))

	if err := os.WriteFile(filename, data, filePermissions); err != nil {
		return "", fmt.Errorf("unable to write chart to cache: %w", err)
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("unable to resolve absolute path for %q: %w", filename, err)
	}

	return abs, nil
}

func (o *OCI) pull(ctx context.Context) ([]byte, error) {
	var opts []registry.ClientOption
	if o.Credentials.hasAuth() {
		opts = append(opts, registry.ClientOptBasicAuth(o.Credentials.Username, o.Credentials.Password))
	}

	rc, err := registry.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to create registry client: %w", err)
	}

	ref, err := o.resolveRef(rc)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before OCI pull: %w", err)
	}

	// Helm registry client APIs used here do not currently accept context.
	// We perform best-effort cancellation checks before and after these calls.
	result, err := rc.Pull(ref)
	if err != nil {
		return nil, fmt.Errorf("unable to pull chart from OCI registry: %w", err)
	}

	if result.Chart == nil || len(result.Chart.Data) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrEmptyChartData, o.Ref)
	}

	return result.Chart.Data, nil
}

func (o *OCI) resolveRef(rc *registry.Client) (string, error) {
	if o.Version != "" {
		return o.Ref + ":" + o.Version, nil
	}

	bareRef := strings.TrimPrefix(o.Ref, "oci://")

	tags, err := rc.Tags(bareRef)
	if err != nil {
		return "", fmt.Errorf("unable to list tags for %q: %w", o.Ref, err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("%w: %q", ErrNoTags, o.Ref)
	}

	latest, err := latestSemver(tags)
	if err != nil {
		return "", fmt.Errorf("unable to resolve latest version for %q: %w", o.Ref, err)
	}

	return o.Ref + ":" + latest.Original(), nil
}
