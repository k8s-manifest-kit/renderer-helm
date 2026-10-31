package helm_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

// The cache collision fixtures build two Helm sources that are indistinguishable
// in every chartSpec field — same chart path, metadata, release, version, and
// default values — while their templates embed a marker so their rendered
// outputs differ. A shared output cache would make the second source reuse the
// first source's result; per-source caches must keep them isolated.

const (
	// cacheCollisionChartName is the chart name shared by every collision
	// fixture.
	cacheCollisionChartName = "cache-app"

	// cacheCollisionChartVersion is the chart version shared by every
	// collision fixture.
	cacheCollisionChartVersion = "1.0.0"

	// cacheCollisionChartRoot is the chart path within the fixture
	// filesystems.
	cacheCollisionChartRoot = "chart"
)

const cacheCollisionChartYAML = `apiVersion: v2
name: cache-app
description: Chart metadata shared by cache collision fixtures
type: application
version: 1.0.0
appVersion: "1.0"
`

const cacheCollisionValuesYAML = `replicaCount: 1
`

// cacheCollisionConfigMapTmpl renders a ConfigMap whose data section embeds
// the fixture marker. The marker comes from the template, not from values, so
// both sources coalesce to identical render values.
const cacheCollisionConfigMapTmpl = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-config
data:
  source: %s
`

// cacheCollisionRepoIndexTmpl is the repository index served by every mock
// Helm repository; its placeholders carry the chart name, chart version, and
// archive name.
const cacheCollisionRepoIndexTmpl = `apiVersion: v1
entries:
  %s:
    - version: %q
      urls:
        - %s
`

// cacheCollisionConfigMapName returns the ConfigMap name rendered by the
// collision fixture for the given release.
func cacheCollisionConfigMapName(releaseName string) string {
	return releaseName + "-config"
}

// cacheCollisionChartFiles returns the chart files of the collision fixture
// for the given marker, keyed by their path relative to the chart root.
func cacheCollisionChartFiles(marker string) map[string]string {
	return map[string]string{
		"Chart.yaml":               cacheCollisionChartYAML,
		"values.yaml":              cacheCollisionValuesYAML,
		"templates/configmap.yaml": fmt.Sprintf(cacheCollisionConfigMapTmpl, marker),
	}
}

// cacheCollisionChartFS returns an in-memory filesystem holding the collision
// fixture chart for the given marker under the shared chart root.
func cacheCollisionChartFS(marker string) fstest.MapFS {
	files := cacheCollisionChartFiles(marker)
	chartFS := make(fstest.MapFS, len(files))

	for name, data := range files {
		chartFS[cacheCollisionChartRoot+"/"+name] = &fstest.MapFile{
			Data: []byte(data),
			Mode: 0o644,
		}
	}

	return chartFS
}

// cacheCollisionChartArchive builds a gzipped tar archive of the collision
// fixture chart for the given marker, with files stored under the chart name
// directory as Helm repository archives expect.
func cacheCollisionChartArchive(t *testing.T, marker string) []byte {
	t.Helper()

	files := cacheCollisionChartFiles(marker)

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		archiveName := cacheCollisionChartName + "/" + name

		header := &tar.Header{
			Name: archiveName,
			Mode: 0o644,
			Size: int64(len(content)),
		}

		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("failed to write archive header %q: %v", archiveName, err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write archive file %q: %v", archiveName, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}

	if err := gw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

// newMockHelmRepo starts a local Helm repository server serving the collision
// fixture chart for the given marker. The server is stopped automatically when
// the test ends, and no external registry or repository is contacted.
func newMockHelmRepo(t *testing.T, marker string) string {
	t.Helper()

	archive := cacheCollisionChartArchive(t, marker)
	archiveName := cacheCollisionChartName + "-" + cacheCollisionChartVersion + ".tgz"
	repoIndex := fmt.Sprintf(cacheCollisionRepoIndexTmpl, cacheCollisionChartName, cacheCollisionChartVersion, archiveName)

	mux := http.NewServeMux()

	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(repoIndex))
	})

	mux.HandleFunc("/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv.URL
}

// cacheCollisionFSSources returns two filesystem sources whose chart path,
// metadata, release, and values are identical; only their template output
// differs through the embedded marker.
func cacheCollisionFSSources(releaseName string) []helm.Source {
	return []helm.Source{
		{
			FS:          cacheCollisionChartFS("alpha"),
			Chart:       cacheCollisionChartRoot,
			ReleaseName: releaseName,
		},
		{
			FS:          cacheCollisionChartFS("beta"),
			Chart:       cacheCollisionChartRoot,
			ReleaseName: releaseName,
		},
	}
}

// cacheCollisionRepoSources returns two repository sources whose chart name,
// version, release, and values are identical; only the template output served
// by the two local mock repositories differs.
func cacheCollisionRepoSources(t *testing.T, releaseName string) []helm.Source {
	t.Helper()

	return []helm.Source{
		{
			Repo:           newMockHelmRepo(t, "alpha"),
			Chart:          cacheCollisionChartName,
			ReleaseName:    releaseName,
			ReleaseVersion: cacheCollisionChartVersion,
		},
		{
			Repo:           newMockHelmRepo(t, "beta"),
			Chart:          cacheCollisionChartName,
			ReleaseName:    releaseName,
			ReleaseVersion: cacheCollisionChartVersion,
		},
	}
}

// constantCacheKey maps every render of a source to the same cache key. It is
// the strongest possible collision across sources and must never make two
// different registered sources share results.
func constantCacheKey(_ any) string {
	return "constant"
}

// labelObjects returns a source post-renderer that sets a fixed label on
// every object.
func labelObjects(name string, value string) types.PostRenderer {
	return func(_ context.Context, objects []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		for i := range objects {
			labels := objects[i].GetLabels()
			if labels == nil {
				labels = make(map[string]string)
			}

			labels[name] = value
			objects[i].SetLabels(labels)
		}

		return objects, nil
	}
}

// assertCollisionRender checks that a render produced exactly one ConfigMap
// per marker, in source order, each embedding its source's marker in the data
// section and carrying the expected render-origin annotation.
func assertCollisionRender(
	g *WithT,
	objects []unstructured.Unstructured,
	releaseName string,
	origin string,
	markers ...string,
) {
	g.Expect(objects).To(HaveLen(len(markers)), "one object per registered source")

	for i, marker := range markers {
		g.Expect(objects[i].GetKind()).To(Equal("ConfigMap"))
		g.Expect(objects[i].GetName()).To(Equal(cacheCollisionConfigMapName(releaseName)))
		g.Expect(configMapData(g, &objects[i])).To(HaveKeyWithValue("source", marker))
	}

	assertRenderOrigin(g, objects, origin)
}

// deploymentReplicas returns the replica count of the single Deployment in a
// render of the simple-app fixture.
func deploymentReplicas(g *WithT, objects []unstructured.Unstructured) int64 {
	for i := range objects {
		if objects[i].GetKind() != "Deployment" {
			continue
		}

		replicas, found, err := unstructured.NestedInt64(objects[i].Object, "spec", "replicas")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue(), "Deployment should set spec.replicas")

		return replicas
	}

	g.Fail("no Deployment rendered from the simple-app fixture")

	return 0
}

// concurrentCacheResult carries one worker's render outcome back to the test
// goroutine, where all assertions are made.
type concurrentCacheResult struct {
	objects []unstructured.Unstructured
	err     error
}
