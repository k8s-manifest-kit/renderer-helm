package helm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/k8s-manifest-kit/engine/pkg/types"
	"github.com/k8s-manifest-kit/pkg/util/k8s"
)

const (
	// maxReleaseNameLength is the maximum allowed length for a Helm release name.
	// This limit is imposed by Kubernetes label value constraints.
	maxReleaseNameLength = 53
)

var (
	// ErrChartEmpty is returned when a chart name is empty or whitespace-only.
	ErrChartEmpty = errors.New("chart cannot be empty or whitespace-only")

	// ErrReleaseNameEmpty is returned when a release name is empty or whitespace-only.
	ErrReleaseNameEmpty = errors.New("release name cannot be empty or whitespace-only")

	// ErrReleaseNameTooLong is returned when a release name exceeds the maximum length.
	ErrReleaseNameTooLong = errors.New("release name exceeds maximum length")
)

// Values returns a Values function that always returns the provided static values.
// This is a convenience helper for the common case of non-dynamic values.
func Values(values map[string]any) func(context.Context) (map[string]any, error) {
	return func(_ context.Context) (map[string]any, error) {
		return values, nil
	}
}

// sourceHolder wraps a Source with internal state for lazy loading and thread-safety.
type sourceHolder struct {
	Source

	// Mutex protects concurrent access to chart field
	mu *sync.RWMutex

	// The loaded Helm chart (protected by mu)
	chart *chart.Chart
}

// Validate checks if the Source configuration is valid.
func (h *sourceHolder) Validate() error {
	if len(strings.TrimSpace(h.Chart)) == 0 {
		return ErrChartEmpty
	}

	releaseName := strings.TrimSpace(h.ReleaseName)
	if len(releaseName) == 0 {
		return ErrReleaseNameEmpty
	}
	if len(releaseName) > maxReleaseNameLength {
		return fmt.Errorf(
			"%w: must not exceed %d characters (got %d)",
			ErrReleaseNameTooLong,
			maxReleaseNameLength,
			len(releaseName),
		)
	}

	return nil
}

// LoadChart returns the loaded Helm chart, loading it lazily if needed.
// Thread-safe for concurrent use.
func (h *sourceHolder) LoadChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.chart != nil {
		return h.chart, nil
	}

	// Check context before starting the load operation
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled during chart load: %w", err)
	}

	opt, err := createChartPathOptions(&h.Source)
	if err != nil {
		return nil, err
	}

	path, err := opt.LocateChart(h.Chart, settings)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to locate chart (repo: %s, name: %s, version: %s): %w",
			h.Repo,
			h.Chart,
			h.ReleaseVersion,
			err,
		)
	}

	c, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load chart (repo: %s, name: %s, version: %s): %w",
			h.Repo,
			h.Chart,
			h.ReleaseVersion,
			err,
		)
	}

	h.chart = c

	return h.chart, nil
}

// createChartPathOptions creates ChartPathOptions for a Source.
// Creates a fresh registry client and install instance per call.
// This allows each Source to have different credential/authentication requirements.
func createChartPathOptions(source *Source) (action.ChartPathOptions, error) {
	c, err := registry.NewClient()
	if err != nil {
		return action.ChartPathOptions{}, fmt.Errorf("unable to create registry client: %w", err)
	}

	install := action.NewInstall(&action.Configuration{
		RegistryClient: c,
	})

	opt := install.ChartPathOptions
	opt.RepoURL = source.Repo
	opt.Version = source.ReleaseVersion

	return opt, nil
}

// addSourceAnnotations adds source tracking annotations to a slice of unstructured objects.
// Only modifies objects if source annotations are enabled in renderer options.
func (r *Renderer) addSourceAnnotations(objects []unstructured.Unstructured, chartPath, fileName string) {
	if !r.opts.SourceAnnotations {
		return
	}

	for i := range objects {
		annotations := objects[i].GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		annotations[types.AnnotationSourceType] = rendererType
		annotations[types.AnnotationSourcePath] = chartPath
		annotations[types.AnnotationSourceFile] = fileName

		objects[i].SetAnnotations(annotations)
	}
}

// processCRDs extracts and processes CRD objects from a Helm chart.
// Returns the decoded unstructured objects with source annotations added if enabled.
func (r *Renderer) processCRDs(helmChart *chart.Chart, holder *sourceHolder) ([]unstructured.Unstructured, error) {
	result := make([]unstructured.Unstructured, 0)

	for _, crd := range helmChart.CRDObjects() {
		objects, err := k8s.DecodeYAML(crd.File.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CRD %s: %w", crd.Name, err)
		}

		r.addSourceAnnotations(objects, holder.Chart, crd.Name)
		result = append(result, objects...)
	}

	return result, nil
}

// processRenderedTemplates extracts and processes rendered template files from Helm output.
// Filters for YAML files, decodes them, and adds source annotations if enabled.
func (r *Renderer) processRenderedTemplates(
	files map[string]string,
	holder *sourceHolder,
) ([]unstructured.Unstructured, error) {
	result := make([]unstructured.Unstructured, 0)

	for k, v := range files {
		if !strings.HasSuffix(k, ".yaml") && !strings.HasSuffix(k, ".yml") {
			continue
		}

		objects, err := k8s.DecodeYAML([]byte(v))
		if err != nil {
			return nil, fmt.Errorf(
				"failed to decode %s: %w",
				k,
				err,
			)
		}

		r.addSourceAnnotations(objects, holder.Chart, k)
		result = append(result, objects...)
	}

	return result, nil
}
