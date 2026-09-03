package locator

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/Masterminds/semver/v3"
)

// Sentinel errors for index resolution failures.
var (
	ErrChartNotFound   = errors.New("chart not found in index")
	ErrVersionNotFound = errors.New("version not found")
)

// Minimal structs for parsing a Helm repository index.yaml without importing
// the repo/v1 package (which transitively pulls in controller-runtime).
//
// These use json struct tags because sigs.k8s.io/yaml (used for unmarshaling)
// converts YAML to JSON internally and then uses encoding/json, so only json
// tags are honored.

type repoIndex struct {
	Entries map[string]repoChartVersions `json:"entries"`
}

type repoChartVersion struct {
	Version string   `json:"version"`
	URLs    []string `json:"urls"`
}

type repoChartVersions []repoChartVersion

func (vs repoChartVersions) Len() int      { return len(vs) }
func (vs repoChartVersions) Swap(i, j int) { vs[i], vs[j] = vs[j], vs[i] }

func (vs repoChartVersions) Less(i, j int) bool {
	return compareRepoChartVersions(vs[i], vs[j]) < 0
}

func compareRepoChartVersions(a, b repoChartVersion) int {
	va, errA := semver.NewVersion(a.Version)
	vb, errB := semver.NewVersion(b.Version)

	if errA != nil || errB != nil {
		return cmp.Compare(a.Version, b.Version)
	}

	return vb.Compare(va)
}

func (idx *repoIndex) resolve(name string, version string) (*repoChartVersion, error) {
	versions, ok := idx.Entries[name]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrChartNotFound, name)
	}

	slices.SortFunc(versions, compareRepoChartVersions)

	if version == "" {
		return &versions[0], nil
	}

	constraint, err := semver.NewConstraint(version)
	if err != nil {
		for i := range versions {
			if versions[i].Version == version {
				return &versions[i], nil
			}
		}

		return nil, fmt.Errorf("%w: %q for chart %q", ErrVersionNotFound, version, name)
	}

	for i := range versions {
		v, err := semver.NewVersion(versions[i].Version)
		if err != nil {
			continue
		}

		if constraint.Check(v) {
			return &versions[i], nil
		}
	}

	return nil, fmt.Errorf("%w: no match for %q in chart %q", ErrVersionNotFound, version, name)
}
