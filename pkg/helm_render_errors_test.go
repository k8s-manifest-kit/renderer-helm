package helm_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

func TestRenderer_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("should handle concurrent loading of non-existent chart", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		invalidPath := "/nonexistent/path/to/chart"
		renderer, err := helm.New([]helm.Source{
			{
				Chart:       invalidPath,
				ReleaseName: "invalid-chart",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Launch multiple concurrent calls to trigger race conditions
		var wg sync.WaitGroup
		errorChan := make(chan error, 5)

		for range 5 {
			wg.Go(func() {
				_, err := renderer.Process(t.Context(), nil)
				errorChan <- err
			})
		}

		wg.Wait()
		close(errorChan)

		// All goroutines should receive proper errors
		errorCount := 0
		for err := range errorChan {
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("error rendering helm chart"))
			errorCount++
		}
		g.Expect(errorCount).To(Equal(5))
	})

	t.Run("should handle charts with ProcessDependencies flag", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// Test that ProcessDependencies flag works with valid charts
		// Note: Dependency resolution errors are typically caught during chart loading
		// This test documents that the flag can be used without panicking
		renderer, err := helm.New([]helm.Source{
			{
				Chart:               testChartPath,
				ReleaseName:         "deps-test",
				ProcessDependencies: true,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Should successfully process chart without dependencies
		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).ToNot(BeEmpty())
	})

	t.Run("should handle malformed YAML in templates", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		tmpDir := t.TempDir()
		chartPath := filepath.Join(tmpDir, "bad-yaml")

		// Create Chart.yaml
		err := os.MkdirAll(filepath.Join(chartPath, "templates"), 0750)
		g.Expect(err).ToNot(HaveOccurred())

		chartYAML := `apiVersion: v2
name: bad-yaml
version: 1.0.0
`
		err = os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte(chartYAML), 0600)
		g.Expect(err).ToNot(HaveOccurred())

		// Create template with malformed YAML (invalid indentation)
		badTemplate := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  invalid: |
    {{- range .Values.items }}
      key: {{ . }}
    indented incorrectly
    {{- end }}
`
		err = os.WriteFile(filepath.Join(chartPath, "templates", "configmap.yaml"), []byte(badTemplate), 0600)
		g.Expect(err).ToNot(HaveOccurred())

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       chartPath,
				ReleaseName: "bad-yaml-test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// This documents expected behavior: Helm may render template successfully
		// even if YAML structure is questionable, as it's up to Kubernetes to validate
		objects, err := renderer.Process(t.Context(), map[string]any{
			"items": []string{"a", "b"},
		})

		// Either succeeds with objects or fails during YAML parsing
		if err != nil {
			g.Expect(err.Error()).To(Or(
				ContainSubstring("yaml"),
				ContainSubstring("unmarshal"),
			))
		} else {
			g.Expect(objects).ToNot(BeNil())
		}
	})

	t.Run("should respect context cancellation during chart loading", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "cancelled-test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(ctx, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("context cancel"))
	})

	t.Run("should provide descriptive errors for invalid chart paths", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		invalidPaths := []string{
			"/nonexistent/chart/path",
			"./missing/local/chart",
			"/tmp/not-a-chart-directory",
		}

		for _, path := range invalidPaths {
			renderer, err := helm.New([]helm.Source{
				{
					Chart:       path,
					ReleaseName: "invalid-path-test",
				},
			})
			g.Expect(err).ToNot(HaveOccurred())

			_, err = renderer.Process(t.Context(), nil)
			g.Expect(err).To(HaveOccurred(), "Expected error for path: %s", path)
			g.Expect(err.Error()).To(ContainSubstring("error rendering helm chart"))

			// Error should include context about which chart/release failed
			g.Expect(err.Error()).To(Or(
				ContainSubstring(path),
				ContainSubstring("invalid-path-test"),
			))
		}
	})

	t.Run("should wrap Values function errors with context", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		customErr := errors.New("database connection failed")
		errorValuesFunc := func(_ context.Context) (types.Values, error) {
			return nil, customErr
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "error-values-test",
				Values:      errorValuesFunc,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())

		// Verify error chain includes original error
		g.Expect(errors.Is(err, customErr)).To(BeTrue(), "Error chain should include original error")

		// Verify error message includes context
		errMsg := err.Error()
		g.Expect(errMsg).To(ContainSubstring("failed to get values"))
		g.Expect(errMsg).To(ContainSubstring("error-values-test"))
	})

	t.Run("should not resolve credentials for local chart paths", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		errorCredsFunc := func(_ context.Context) (*locator.Credentials, error) {
			return nil, errors.New("authentication service unavailable")
		}

		renderer, err := helm.New([]helm.Source{
			{
				Chart:       testChartPath,
				ReleaseName: "error-creds-test",
				Credentials: errorCredsFunc,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Local chart paths skip credential resolution entirely.
		objects, err := renderer.Process(t.Context(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objects).To(HaveLen(3))
	})
}

func TestFilesystemErrors(t *testing.T) {
	t.Run("should reject a missing filesystem chart root", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				FS:          fstest.MapFS{},
				Chart:       "missing",
				ReleaseName: "test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to walk chart root"))
	})

	t.Run("should reject a filesystem chart without Chart.yaml", func(t *testing.T) {
		g := NewWithT(t)
		renderer, err := helm.New([]helm.Source{
			{
				FS: fstest.MapFS{
					"chart/templates/ignored.yaml": &fstest.MapFile{
						Data: []byte("apiVersion: v1\nkind: ConfigMap\n"),
					},
				},
				Chart:       "chart",
				ReleaseName: "test",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("Chart.yaml file is missing"))
	})
}
