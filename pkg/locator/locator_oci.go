package locator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
)

// ErrEmptyChartData is returned when an OCI pull returns no chart content.
var ErrEmptyChartData = errors.New("OCI pull returned empty chart data")

// ErrNoTags is returned when a registry has no tags for the given reference.
var ErrNoTags = errors.New("no tags found")

// ErrRefContainsTag is returned when an OCI ref already contains an embedded
// tag and a separate Version is also specified, which would produce an invalid reference.
var ErrRefContainsTag = errors.New("OCI ref already contains a tag; cannot also specify Version")

// OCI resolves charts stored in an OCI-compatible registry.
type OCI struct {
	Ref         string
	Version     string
	Credentials *Credentials
	CacheDir    string
}

// Locate pulls the chart from an OCI registry and returns the local cache path.
func (o *OCI) Locate(ctx context.Context) (Result, error) {
	if err := os.MkdirAll(o.CacheDir, dirPermissions); err != nil {
		return Result{}, fmt.Errorf("unable to create repository cache directory: %w", err)
	}

	data, err := o.pull(ctx)
	if err != nil {
		return Result{}, err
	}

	hash := sha256.Sum256(data)
	filename := filepath.Join(o.CacheDir, fmt.Sprintf("%x.tgz", hash))

	if err := os.WriteFile(filename, data, filePermissions); err != nil {
		return Result{}, fmt.Errorf("unable to write chart to cache: %w", err)
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		return Result{}, fmt.Errorf("unable to resolve absolute path for %q: %w", filename, err)
	}

	return Result{Path: abs, SourceType: SourceOCI}, nil
}

func (o *OCI) pull(ctx context.Context) ([]byte, error) {
	var opts []ClientOption
	if o.Credentials.hasAuth() {
		opts = append(opts, WithCredentials(o.Credentials))
	}

	client, err := NewOCIClient(o.Ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to create OCI client: %w", err)
	}

	tag, err := o.resolveTag(ctx, client)
	if err != nil {
		return nil, err
	}

	data, err := client.Pull(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("unable to pull chart from OCI registry: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrEmptyChartData, o.Ref)
	}

	return data, nil
}

func (o *OCI) resolveTag(ctx context.Context, client *Client) (string, error) {
	tag := o.embeddedTag()

	switch {
	case tag != "" && o.Version != "":
		return "", fmt.Errorf("%w: ref %q has tag %q, version %q", ErrRefContainsTag, o.Ref, tag, o.Version)
	case o.Version != "":
		return o.Version, nil
	case tag != "":
		return tag, nil
	}

	tags, err := client.Tags(ctx)

	switch {
	case err != nil:
		return "", fmt.Errorf("unable to list tags for %q: %w", o.Ref, err)
	case len(tags) == 0:
		return "", fmt.Errorf("%w: %q", ErrNoTags, o.Ref)
	}

	latest, err := latestSemver(tags)
	if err != nil {
		return "", fmt.Errorf("unable to resolve latest version for %q: %w", o.Ref, err)
	}

	return latest.Original(), nil
}

// embeddedTag extracts a tag from the OCI ref if present.
// e.g. "oci://registry/chart:1.0" returns "1.0", "oci://registry/chart" returns "".
// Port numbers (e.g. "localhost:5000/chart") are not mistaken for tags.
func (o *OCI) embeddedTag() string {
	named, err := reference.ParseNormalizedNamed(strings.TrimPrefix(o.Ref, "oci://"))
	if err != nil {
		return ""
	}

	tagged, ok := named.(reference.Tagged)
	if !ok {
		return ""
	}

	return tagged.Tag()
}
