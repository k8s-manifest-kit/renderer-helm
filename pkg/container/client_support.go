package container

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/distribution/reference"
	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

const (
	chartLayerMediaType         = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	legacyChartLayerMediaType   = "application/tar+gzip"
	dockerManifestListMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"

	maxBlobSize = 256 << 20 // 256 MiB hard upper bound for blob downloads

	// maxMetadataSize bounds manifest and index downloads. Metadata
	// documents are small; the cap keeps a hostile descriptor from driving
	// large metadata reads or allocations before verification can reject
	// its content.
	maxMetadataSize = 4 << 20 // 4 MiB
)

// computeCredentials returns a credential function for OCI registry auth.
// When no explicit credential is provided, it attempts to load the Docker
// credential store. Initialization failures are intentionally swallowed
// (falling back to empty credentials) so that pulls from public registries
// work even when no Docker config exists. AllowPlaintextPut is required by
// the oras credentials API for read-only store access; it does not affect
// security since this client never writes credentials.
func computeCredentials(named reference.Named, opts *ClientOptions) auth.CredentialFunc {
	if opts.credential != nil {
		host := reference.Domain(named)

		return auth.StaticCredential(host, *opts.credential)
	}

	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		AllowPlaintextPut:        true,
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		return func(_ context.Context, _ string) (auth.Credential, error) {
			return auth.EmptyCredential, nil
		}
	}

	return credentials.Credential(store)
}

func listTags(ctx context.Context, repo *remote.Repository) ([]string, error) {
	var tags []string

	err := repo.Tags(ctx, "", func(page []string) error {
		for _, tag := range page {
			tags = append(tags, strings.ReplaceAll(tag, "_", "+"))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	return tags, nil
}

func fetchManifest(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (*resolvedArtifact, error) {
	manifest, err := fetchJSON[ocispec.Manifest](ctx, repo, desc)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch manifest: %w", err)
	}

	return &resolvedArtifact{desc: desc, manifest: manifest}, nil
}

// fetchBlob downloads the chart layer described by desc, verifying the bytes
// against the descriptor before returning them.
func fetchBlob(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	if err := checkDescriptorSize(desc, maxBlobSize, ErrBlobTooLarge); err != nil {
		return nil, err
	}

	return fetchVerified(ctx, repo, desc)
}

// fetchManifestBytes downloads the raw bytes of the manifest or index
// described by desc, verifying them against the descriptor. Metadata is
// bounded well below the blob limit so a hostile descriptor cannot drive
// large metadata reads.
func fetchManifestBytes(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	if err := checkDescriptorSize(desc, maxMetadataSize, ErrManifestTooLarge); err != nil {
		return nil, err
	}

	return fetchVerified(ctx, repo, desc)
}

// fetchJSON downloads the JSON document described by desc, verifying the
// body against the descriptor before decoding it.
func fetchJSON[T any](ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (T, error) {
	var zero T

	data, err := fetchManifestBytes(ctx, repo, desc)
	if err != nil {
		return zero, err
	}

	var target T
	if err := json.Unmarshal(data, &target); err != nil {
		return zero, fmt.Errorf("decode %s: %w", desc.Digest, err)
	}

	return target, nil
}

// fetchVerified downloads the content described by desc and returns its
// bytes only after they verify against the descriptor's digest and exact
// size. The response body is closed on success and error. The caller must
// bound desc.Size first: the read is capped at desc.Size, so a validated
// bound also bounds allocation.
func fetchVerified(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) ([]byte, error) {
	if err := desc.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidDigest, desc.Digest, err)
	}

	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", desc.Digest, err)
	}
	defer func() { _ = rc.Close() }()

	// content.ReadAll verifies the exact size and digest of the body while
	// capping the initial buffer allocation, so a hostile descriptor cannot
	// force a huge allocation before verification rejects its content.
	data, err := content.ReadAll(rc, desc)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrContentMismatch, desc.Digest, err)
	}

	return data, nil
}

// checkDescriptorSize rejects a descriptor whose size is unusable or exceeds
// maxSize, before any bytes are fetched or allocated. tooLarge selects the
// sentinel error reported for the max violation.
func checkDescriptorSize(desc ocispec.Descriptor, maxSize int64, tooLarge error) error {
	if desc.Size <= 0 {
		return fmt.Errorf("%w: %s reports size %d", ErrInvalidDescriptorSize, desc.Digest, desc.Size)
	}

	if desc.Size > maxSize {
		return fmt.Errorf("%w: %s is %d bytes (max %d)", tooLarge, desc.Digest, desc.Size, maxSize)
	}

	return nil
}

func isIndexType(mediaType string) bool {
	return mediaType == ocispec.MediaTypeImageIndex || mediaType == dockerManifestListMediaType
}

func isChartType(mediaType string) bool {
	return mediaType == chartLayerMediaType || mediaType == legacyChartLayerMediaType
}

// findChartLayer returns the last layer with a recognised Helm chart media
// type, or nil if the manifest contains no chart layer.  The last match is
// chosen because Helm's own push places the chart content as the final layer,
// and some tooling prepends non-chart layers.
func findChartLayer(manifest ocispec.Manifest) *ocispec.Descriptor {
	var layer *ocispec.Descriptor
	for i := range manifest.Layers {
		if isChartType(manifest.Layers[i].MediaType) {
			layer = &manifest.Layers[i]
		}
	}

	return layer
}

// resolveTag determines the tag to pull. Digest refs are never passed here --
// the caller (locator_oci.pull) checks for an embedded digest first and routes
// to PullDigest, bypassing tag resolution entirely.
func resolveTag(
	ctx context.Context,
	ref string,
	version string,
	listTagsFn func(context.Context) ([]string, error),
) (string, error) {
	tag := EmbeddedTag(ref)

	switch {
	case tag != "" && version != "":
		return "", fmt.Errorf("%w: ref %q has tag %q, version %q", ErrRefContainsTag, ref, tag, version)
	case version != "":
		return version, nil
	case tag != "":
		return tag, nil
	}

	tags, err := listTagsFn(ctx)

	switch {
	case err != nil:
		return "", fmt.Errorf("unable to list tags for %q: %w", ref, err)
	case len(tags) == 0:
		return "", fmt.Errorf("%w: %q", ErrNoTags, ref)
	}

	latest, err := latestSemver(tags)
	if err != nil {
		return "", fmt.Errorf("unable to resolve latest version for %q: %w", ref, err)
	}

	return latest.Original(), nil
}

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

	slices.SortFunc(versions, func(a, b *semver.Version) int {
		return b.Compare(a)
	})

	return versions[0], nil
}

// EmbeddedTag extracts a tag from an OCI ref if present.
// e.g. "oci://registry/chart:1.0" returns "1.0", "oci://registry/chart" returns "".
// Port numbers (e.g. "localhost:5000/chart") are not mistaken for tags.
func EmbeddedTag(ref string) string {
	named, err := reference.ParseNormalizedNamed(strings.TrimPrefix(ref, "oci://"))
	if err != nil {
		return ""
	}

	tagged, ok := named.(reference.Tagged)
	if !ok {
		return ""
	}

	return tagged.Tag()
}

// EmbeddedDigest extracts a digest from an OCI ref if present.
// e.g. "oci://registry/chart@sha256:abc..." returns the digest,
// "oci://registry/chart:1.0" returns "".
func EmbeddedDigest(ref string) godigest.Digest {
	named, err := reference.ParseNormalizedNamed(strings.TrimPrefix(ref, "oci://"))
	if err != nil {
		return ""
	}

	digested, ok := named.(reference.Digested)
	if !ok {
		return ""
	}

	return digested.Digest()
}
