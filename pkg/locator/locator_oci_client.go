package locator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/k8s-manifest-kit/pkg/util"
)

const (
	chartLayerMediaType       = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	legacyChartLayerMediaType = "application/tar+gzip"
)

// ErrNoChartLayer is returned when the OCI manifest contains no layer with
// a recognised Helm chart media type.
var ErrNoChartLayer = errors.New("no chart layer found in OCI manifest")

// ClientOption is a functional option for the OCI Client.
type ClientOption = util.Option[ClientOptions]

// ClientOptions holds configuration for the OCI registry client.
type ClientOptions struct {
	Credentials *Credentials
	PlainHTTP   bool
}

// ApplyTo applies the client options to the target configuration.
func (opts ClientOptions) ApplyTo(target *ClientOptions) {
	if opts.Credentials != nil {
		target.Credentials = opts.Credentials
	}

	if opts.PlainHTTP {
		target.PlainHTTP = opts.PlainHTTP
	}
}

// WithCredentials sets the authentication credentials for the OCI client.
func WithCredentials(creds *Credentials) ClientOption {
	return util.FunctionalOption[ClientOptions](func(opts *ClientOptions) {
		opts.Credentials = creds
	})
}

// WithPlainHTTP enables plain HTTP (non-TLS) communication with the registry.
func WithPlainHTTP(plain bool) ClientOption {
	return util.FunctionalOption[ClientOptions](func(opts *ClientOptions) {
		opts.PlainHTTP = plain
	})
}

// Client wraps an ORAS remote.Repository for Helm chart OCI operations.
type Client struct {
	ref  string
	repo *remote.Repository
}

// NewOCIClient creates a new OCI registry client for the given reference.
// The ref may include an oci:// prefix and/or an embedded tag; both are
// stripped. Tags are passed separately to Pull.
func NewOCIClient(ref string, opts ...ClientOption) (*Client, error) {
	var options ClientOptions
	for _, opt := range opts {
		opt.ApplyTo(&options)
	}

	bareRef := strings.TrimPrefix(ref, "oci://")

	named, err := reference.ParseNormalizedNamed(bareRef)
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference %q: %w", ref, err)
	}

	repoRef := reference.Domain(named) + "/" + reference.Path(named)

	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return nil, fmt.Errorf("unable to create repository for %q: %w", ref, err)
	}

	repo.PlainHTTP = options.PlainHTTP

	repo.Client = &auth.Client{Credential: computeCredentials(named, &options)}

	return &Client{ref: repoRef, repo: repo}, nil
}

func computeCredentials(named reference.Named, opts *ClientOptions) auth.CredentialFunc {
	if opts.Credentials.hasAuth() {
		host := reference.Domain(named)

		return auth.StaticCredential(host, auth.Credential{
			Username: opts.Credentials.Username,
			Password: opts.Credentials.Password,
		})
	}

	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		AllowPlaintextPut:        true,
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		return nil
	}

	return credentials.Credential(store)
}

// Pull fetches the Helm chart .tgz bytes for the given tag.
// Only the chart layer is downloaded; config and provenance layers are skipped.
func (c *Client) Pull(ctx context.Context, tag string) ([]byte, error) {
	fullRef := c.ref + ":" + tag

	manifestDesc, err := c.repo.Resolve(ctx, fullRef)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve %q: %w", fullRef, err)
	}

	rc, err := c.repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch manifest for %q: %w", fullRef, err)
	}
	defer func() { _ = rc.Close() }()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("unable to parse manifest for %q: %w", fullRef, err)
	}

	var chartLayer *ocispec.Descriptor
	for i := range manifest.Layers {
		mt := manifest.Layers[i].MediaType
		if mt == chartLayerMediaType || mt == legacyChartLayerMediaType {
			chartLayer = &manifest.Layers[i]
		}
	}

	if chartLayer == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoChartLayer, fullRef)
	}

	blobRC, err := c.repo.Fetch(ctx, *chartLayer)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch chart layer for %q: %w", fullRef, err)
	}
	defer func() { _ = blobRC.Close() }()

	data, err := io.ReadAll(blobRC)
	if err != nil {
		return nil, fmt.Errorf("unable to read chart layer for %q: %w", fullRef, err)
	}

	return data, nil
}

// Tags returns all tags from the repository, converting OCI tag underscores
// back to plus signs for semver compatibility.
// See https://github.com/helm/helm/issues/10166
func (c *Client) Tags(ctx context.Context) ([]string, error) {
	var tags []string

	err := c.repo.Tags(ctx, "", func(page []string) error {
		for _, tag := range page {
			tags = append(tags, strings.ReplaceAll(tag, "_", "+"))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list tags for %q: %w", c.ref, err)
	}

	return tags, nil
}
