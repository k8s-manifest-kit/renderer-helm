package fs_test

import (
	"bytes"
	"fmt"
	iofs "io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/k8s-manifest-kit/renderer-helm/pkg/fs"

	. "github.com/onsi/gomega"
)

const utf8BOM = "\xEF\xBB\xBF"

const configmapTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
data:
  value: {{ .Values.setting }}
`

func minimalChartFS() fstest.MapFS {
	return fstest.MapFS{
		"chart/Chart.yaml": &fstest.MapFile{
			Data: []byte("apiVersion: v2\nname: minimal\nversion: 0.1.0\n"),
			Mode: 0o644,
		},
		"chart/values.yaml": &fstest.MapFile{
			Data: []byte("setting: base\n"),
			Mode: 0o644,
		},
		"chart/templates/configmap.yaml": &fstest.MapFile{
			Data: []byte(fmt.Sprintf(configmapTemplate, "minimal-config")),
			Mode: 0o644,
		},
	}
}

func TestLoadChartFromFS(t *testing.T) {
	t.Run("should load a minimal chart", func(t *testing.T) {
		g := NewWithT(t)

		loaded, err := fs.LoadChartFromFS(minimalChartFS(), "chart")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(loaded.Name()).To(Equal("minimal"))
		g.Expect(loaded.Metadata.Version).To(Equal("0.1.0"))
		g.Expect(loaded.Values).To(HaveKeyWithValue("setting", "base"))

		g.Expect(loaded.Templates).To(HaveLen(1))
		g.Expect(loaded.Templates[0].Name).To(Equal("templates/configmap.yaml"))
	})

	t.Run("should honor helmignore rules for files and directories", func(t *testing.T) {
		g := NewWithT(t)

		chartFS := minimalChartFS()
		chartFS["chart/.helmignore"] = &fstest.MapFile{
			Data: []byte("templates/ignored.yaml\nsecret\n"),
			Mode: 0o644,
		}
		chartFS["chart/templates/ignored.yaml"] = &fstest.MapFile{
			Data: []byte("{{ invalid template"),
			Mode: 0o644,
		}
		chartFS["chart/secret/leak.yaml"] = &fstest.MapFile{
			Data: []byte("apiVersion: v1\nkind: Secret\n"),
			Mode: 0o644,
		}

		loaded, err := fs.LoadChartFromFS(chartFS, "chart")
		g.Expect(err).ToNot(HaveOccurred())

		for _, template := range loaded.Templates {
			g.Expect(template.Name).ToNot(Equal("templates/ignored.yaml"))
		}
		for _, raw := range loaded.Raw {
			g.Expect(raw.Name).ToNot(HavePrefix("secret/"))
			g.Expect(raw.Name).ToNot(Equal("templates/ignored.yaml"))
		}

		g.Expect(loaded.Templates).To(HaveLen(1))
		g.Expect(loaded.Templates[0].Name).To(Equal("templates/configmap.yaml"))
	})

	t.Run("should strip a UTF-8 BOM from chart files", func(t *testing.T) {
		g := NewWithT(t)

		expected := fmt.Sprintf(configmapTemplate, "bom-config")

		chartFS := minimalChartFS()
		chartFS["chart/templates/configmap.yaml"] = &fstest.MapFile{
			Data: []byte(utf8BOM + expected),
			Mode: 0o644,
		}

		loaded, err := fs.LoadChartFromFS(chartFS, "chart")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(loaded.Templates).To(HaveLen(1))
		g.Expect(loaded.Templates[0].Name).To(Equal("templates/configmap.yaml"))
		g.Expect(string(loaded.Templates[0].Data)).To(Equal(expected))
		g.Expect(bytes.HasPrefix(loaded.Templates[0].Data, []byte(utf8BOM))).To(BeFalse())
	})

	t.Run("should fail on a missing chart root", func(t *testing.T) {
		g := NewWithT(t)

		_, err := fs.LoadChartFromFS(minimalChartFS(), "does-not-exist")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to walk chart root"))
		g.Expect(err.Error()).To(ContainSubstring("file does not exist"))
	})

	t.Run("should fail on an invalid chart root", func(t *testing.T) {
		g := NewWithT(t)

		_, err := fs.LoadChartFromFS(minimalChartFS(), "../escape")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to open chart root"))
	})

	t.Run("should fail on malformed ignore rules", func(t *testing.T) {
		g := NewWithT(t)

		chartFS := minimalChartFS()
		chartFS["chart/.helmignore"] = &fstest.MapFile{
			Data: []byte("templates/**\n"),
			Mode: 0o644,
		}

		_, err := fs.LoadChartFromFS(chartFS, "chart")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to parse .helmignore"))
	})

	t.Run("should fail on irregular chart files", func(t *testing.T) {
		g := NewWithT(t)

		chartFS := minimalChartFS()
		chartFS["chart/templates/pipe.yaml"] = &fstest.MapFile{
			Data: []byte("apiVersion: v1\n"),
			Mode: 0o644,
		}

		_, err := fs.LoadChartFromFS(irregularFileFS{inner: chartFS}, "chart")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("cannot load irregular file"))
		g.Expect(err.Error()).To(ContainSubstring("pipe.yaml"))
	})

	t.Run("should fail on chart files above the decompressed size limit", func(t *testing.T) {
		g := NewWithT(t)

		chartFS := minimalChartFS()
		chartFS["chart/templates/oversized.yaml"] = &fstest.MapFile{
			Data: bytes.Repeat([]byte("x"), 5*1024*1024+1),
			Mode: 0o644,
		}

		_, err := fs.LoadChartFromFS(chartFS, "chart")
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("chart file is larger than the maximum file size"))
	})
}

// irregularFileFS reports the pipe.yaml entry as a named pipe instead of a
// regular file, exercising the loader's irregular-file guard without a real
// filesystem. It deliberately delegates Open, ReadDir, and ReadFile without
// promoting the inner filesystem's Sub method, so the loader's fs.Sub call
// wraps this filesystem and observes the rewritten directory entries.
type irregularFileFS struct {
	inner iofs.FS
}

func (f irregularFileFS) Open(name string) (iofs.File, error) {
	return f.inner.Open(name) //nolint:wrapcheck // fixture delegates pass-through errors unchanged
}

func (f irregularFileFS) ReadFile(name string) ([]byte, error) {
	return iofs.ReadFile(f.inner, name) //nolint:wrapcheck // fixture delegates pass-through errors unchanged
}

func (f irregularFileFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	entries, err := iofs.ReadDir(f.inner, name)
	if err != nil {
		return nil, err //nolint:wrapcheck // fixture delegates pass-through errors unchanged
	}

	reported := make([]iofs.DirEntry, len(entries))
	for i, entry := range entries {
		if entry.Name() == "pipe.yaml" {
			reported[i] = pipeEntry{entry}
		} else {
			reported[i] = entry
		}
	}

	return reported, nil
}

// pipeEntry swaps the entry's file info for a named pipe.
type pipeEntry struct {
	iofs.DirEntry
}

func (pipeEntry) Info() (iofs.FileInfo, error) {
	return pipeInfo{}, nil
}

type pipeInfo struct{}

func (pipeInfo) Name() string        { return "pipe.yaml" }
func (pipeInfo) Size() int64         { return 0 }
func (pipeInfo) Mode() iofs.FileMode { return iofs.ModeNamedPipe }
func (pipeInfo) ModTime() time.Time  { return time.Time{} }
func (pipeInfo) IsDir() bool         { return false }
func (pipeInfo) Sys() any            { return nil }
