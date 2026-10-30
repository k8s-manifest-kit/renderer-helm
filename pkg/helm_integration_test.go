//go:build integration

package helm_test

import (
	"fmt"
	"testing"

	"github.com/rs/xid"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// Live chart inputs pinned to exact published versions so the integration
// suite stays reproducible. Verify a version still exists in the target
// registry or repository index before changing it.
const (
	// bitnamiChartsNginxVersion is served both from
	// oci://registry-1.docker.io/bitnamicharts/nginx and
	// https://charts.bitnami.com/bitnami.
	bitnamiChartsNginxVersion = "18.1.0"

	// daprSharedChartVersion is served from
	// oci://registry-1.docker.io/daprio/dapr-shared-chart.
	daprSharedChartVersion = "0.0.16"
)

func TestRendererRemoteSources(t *testing.T) {
	t.Parallel()

	t.Run("should render chart from OCI registry with correct content", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		renderer, err := helm.New(
			[]helm.Source{{
				Chart:          "oci://registry-1.docker.io/bitnamicharts/nginx",
				ReleaseName:    xid.New().String(),
				ReleaseVersion: bitnamiChartsNginxVersion,
			}},
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		var deploymentFound bool

		for _, obj := range objects {
			gvk := obj.GroupVersionKind()
			if gvk.Group == appsv1.GroupName && gvk.Kind == "Deployment" {
				lbls := obj.GetLabels()
				if lbls["app.kubernetes.io/name"] == "nginx" {
					deploymentFound = true

					break
				}
			}
		}

		g.Expect(deploymentFound).To(BeTrue(), "expected at least one Deployment with app.kubernetes.io/name=nginx")
	})

	t.Run("should render chart from Helm repository with correct content", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		renderer, err := helm.New(
			[]helm.Source{{
				Repo:           "https://charts.bitnami.com/bitnami",
				Chart:          "nginx",
				ReleaseName:    xid.New().String(),
				ReleaseVersion: bitnamiChartsNginxVersion,
			}},
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		var deploymentFound bool

		for _, obj := range objects {
			gvk := obj.GroupVersionKind()
			if gvk.Group == appsv1.GroupName && gvk.Kind == "Deployment" {
				lbls := obj.GetLabels()
				if lbls["app.kubernetes.io/name"] == "nginx" {
					deploymentFound = true

					break
				}
			}
		}

		g.Expect(deploymentFound).To(BeTrue(), "expected at least one Deployment with app.kubernetes.io/name=nginx")
	})

	t.Run("OCI and Repo should produce identical resource sets for same chart version", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		releaseName := xid.New().String()

		ociRenderer, err := helm.New(
			[]helm.Source{{
				Chart:          "oci://registry-1.docker.io/bitnamicharts/nginx",
				ReleaseName:    releaseName,
				ReleaseVersion: bitnamiChartsNginxVersion,
			}},
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		repoRenderer, err := helm.New(
			[]helm.Source{{
				Repo:           "https://charts.bitnami.com/bitnami",
				Chart:          "nginx",
				ReleaseName:    releaseName,
				ReleaseVersion: bitnamiChartsNginxVersion,
			}},
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		ociObjects, err := ociRenderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		repoObjects, err := repoRenderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(ociObjects).To(HaveLen(len(repoObjects)),
			"OCI and Repo should produce the same number of resources for the same chart version")

		ociIDs := objectIdentities(ociObjects)
		repoIDs := objectIdentities(repoObjects)

		g.Expect(ociIDs).To(ConsistOf(repoIDs),
			"OCI and Repo should produce the same resource identities (GVK + namespace/name)")
	})
}

// objectIdentities returns the "group/version/kind/namespace/name" identity
// of every object, suitable for comparing resource sets independent of order.
func objectIdentities(objects []unstructured.Unstructured) []string {
	ids := make([]string, 0, len(objects))
	for _, obj := range objects {
		objGVK := obj.GroupVersionKind()
		ids = append(ids, fmt.Sprintf("%s/%s/%s/%s/%s",
			objGVK.Group, objGVK.Version, objGVK.Kind,
			obj.GetNamespace(), obj.GetName()))
	}

	return ids
}

func TestRenderer_Integration(t *testing.T) {
	t.Parallel()

	t.Run("should render chart from OCI registry", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				Chart:          "oci://registry-1.docker.io/daprio/dapr-shared-chart",
				ReleaseVersion: daprSharedChartVersion,
				ReleaseName:    "test-release",
				Values: helm.Values(map[string]any{
					"shared": map[string]any{
						"appId": "test-app",
					},
				}),
			},
		},
			helm.WithRepositoryCache(t.TempDir()),
		)
		g.Expect(err).ToNot(HaveOccurred())

		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())

		// Check that resources were rendered
		found := false
		for _, obj := range objects {
			if obj.GetKind() == "Deployment" || obj.GetKind() == "Service" {
				found = true

				break
			}
		}
		g.Expect(found).To(BeTrue(), "Should have rendered at least one Deployment or Service")
	})
}
