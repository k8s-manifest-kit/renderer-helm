package container

import (
	"errors"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ErrNoChartLayer is returned when the OCI manifest contains no layer with
// a recognised Helm chart media type.
var ErrNoChartLayer = errors.New("no chart layer found in OCI manifest")

// ErrEmptyIndex is returned when an OCI image index contains no manifest entries.
var ErrEmptyIndex = errors.New("OCI index contains no manifests")

// ErrEmptyTag is returned when Pull is called with an empty tag.
var ErrEmptyTag = errors.New("tag must not be empty")

// ErrInvalidDigest is returned when a digest is malformed, either one
// passed to PullDigest or one recorded in a content descriptor.
var ErrInvalidDigest = errors.New("invalid digest")

// ErrRefContainsTag is returned when an OCI ref already contains an embedded
// tag and a separate version is also specified.
var ErrRefContainsTag = errors.New("OCI ref already contains a tag; cannot also specify Version")

// ErrRefContainsDigest is returned when an OCI ref contains an embedded digest
// and a separate version is also specified.
var ErrRefContainsDigest = errors.New("OCI ref already contains a digest; cannot also specify Version")

// ErrNoTags is returned when a registry has no tags for the given reference.
var ErrNoTags = errors.New("no tags found")

// ErrNoValidSemverTag is returned when none of the registry tags are valid semver.
var ErrNoValidSemverTag = errors.New("no valid semver tag found")

// ErrInvalidDescriptorSize is returned when a descriptor reports a non-positive size.
var ErrInvalidDescriptorSize = errors.New("descriptor has invalid size")

// ErrBlobTooLarge is returned when a blob exceeds the maximum allowed size.
var ErrBlobTooLarge = errors.New("blob exceeds maximum allowed size")

// ErrManifestTooLarge is returned when a manifest or index descriptor
// exceeds the maximum allowed metadata size.
var ErrManifestTooLarge = errors.New("manifest exceeds maximum allowed size")

// ErrContentMismatch is returned when downloaded OCI content fails
// verification against its descriptor — the bytes hash to a different
// digest, or the body is shorter or longer than the descriptor size — or
// when a digest-pinned reference resolves to a different digest than the
// one requested.
var ErrContentMismatch = errors.New("OCI content does not match its descriptor")

type resolvedArtifact struct {
	desc     ocispec.Descriptor
	manifest ocispec.Manifest
}
