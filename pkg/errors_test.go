package helm_test

import (
	"errors"
	"fmt"
	"testing"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/locator"

	. "github.com/onsi/gomega"
)

func TestErrorTypes(t *testing.T) {
	t.Run("ValidationError wraps sentinel via errors.Is", func(t *testing.T) {
		g := NewWithT(t)

		_, err := helm.New([]helm.Source{
			{Chart: "", ReleaseName: "test"},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, helm.ErrChartEmpty)).To(BeTrue())

		var ve *helm.ValidationError
		g.Expect(errors.As(err, &ve)).To(BeTrue())
		g.Expect(ve.Field).To(Equal("Chart"))
	})

	t.Run("ValidationError for empty release name", func(t *testing.T) {
		g := NewWithT(t)

		_, err := helm.New([]helm.Source{
			{Chart: "oci://example/chart", ReleaseName: ""},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, helm.ErrReleaseNameEmpty)).To(BeTrue())

		var ve *helm.ValidationError
		g.Expect(errors.As(err, &ve)).To(BeTrue())
		g.Expect(ve.Field).To(Equal("ReleaseName"))
	})

	t.Run("ValidationError for release name too long", func(t *testing.T) {
		g := NewWithT(t)

		longName := "a"
		for len(longName) <= 53 {
			longName += "a"
		}

		_, err := helm.New([]helm.Source{
			{Chart: "oci://example/chart", ReleaseName: longName},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, helm.ErrReleaseNameTooLong)).To(BeTrue())

		var ve *helm.ValidationError
		g.Expect(errors.As(err, &ve)).To(BeTrue())
		g.Expect(ve.Field).To(Equal("ReleaseName"))
	})

	t.Run("ValidationError for invalid release name format", func(t *testing.T) {
		g := NewWithT(t)

		_, err := helm.New([]helm.Source{
			{Chart: "oci://example/chart", ReleaseName: "INVALID_NAME"},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.Is(err, helm.ErrReleaseNameInvalidFormat)).To(BeTrue())

		var ve *helm.ValidationError
		g.Expect(errors.As(err, &ve)).To(BeTrue())
		g.Expect(ve.Field).To(Equal("ReleaseName"))
	})

	t.Run("LocateError wraps inner errors via errors.Is", func(t *testing.T) {
		g := NewWithT(t)

		inner := fmt.Errorf("locate failed: %w", locator.ErrPathNotFound)
		le := &helm.LocateError{
			Chart:   "oci://example/chart",
			Repo:    "",
			Version: "1.0.0",
			Err:     inner,
		}

		g.Expect(errors.Is(le, locator.ErrPathNotFound)).To(BeTrue())

		var target *helm.LocateError
		g.Expect(errors.As(le, &target)).To(BeTrue())
		g.Expect(target.Chart).To(Equal("oci://example/chart"))
		g.Expect(target.Version).To(Equal("1.0.0"))
	})

	t.Run("LocateError discovered through fmt.Errorf wrapping", func(t *testing.T) {
		g := NewWithT(t)

		le := &helm.LocateError{
			Chart: "oci://example/chart",
			Err:   locator.ErrChartNotFound,
		}
		wrapped := fmt.Errorf("outer: %w", le)

		g.Expect(errors.Is(wrapped, locator.ErrChartNotFound)).To(BeTrue())
		g.Expect(helm.IsLocateError(wrapped)).To(BeTrue())
	})

	t.Run("RenderError wraps inner errors", func(t *testing.T) {
		g := NewWithT(t)

		inner := errors.New("template error")
		re := &helm.RenderError{
			Chart:       "oci://example/chart",
			ReleaseName: "my-release",
			Err:         inner,
		}

		var target *helm.RenderError
		g.Expect(errors.As(re, &target)).To(BeTrue())
		g.Expect(target.Chart).To(Equal("oci://example/chart"))
		g.Expect(target.ReleaseName).To(Equal("my-release"))
		g.Expect(re.Error()).To(Equal("template error"))
	})

	t.Run("RenderError discovered through fmt.Errorf wrapping", func(t *testing.T) {
		g := NewWithT(t)

		re := &helm.RenderError{
			Chart:       "oci://example/chart",
			ReleaseName: "my-release",
			Err:         errors.New("decode failed"),
		}
		wrapped := fmt.Errorf("error rendering helm chart: %w", re)

		g.Expect(helm.IsRenderError(wrapped)).To(BeTrue())

		var target *helm.RenderError
		g.Expect(errors.As(wrapped, &target)).To(BeTrue())
		g.Expect(target.ReleaseName).To(Equal("my-release"))
	})

	t.Run("IsValidationError predicate", func(t *testing.T) {
		g := NewWithT(t)

		ve := &helm.ValidationError{Field: "Chart", Err: helm.ErrChartEmpty}
		wrapped := fmt.Errorf("creation failed: %w", ve)

		g.Expect(helm.IsValidationError(wrapped)).To(BeTrue())
		g.Expect(helm.IsValidationError(errors.New("unrelated"))).To(BeFalse())
	})

	t.Run("IsLocateError predicate", func(t *testing.T) {
		g := NewWithT(t)

		le := &helm.LocateError{Chart: "x", Err: errors.New("network")}
		wrapped := fmt.Errorf("failed: %w", le)

		g.Expect(helm.IsLocateError(wrapped)).To(BeTrue())
		g.Expect(helm.IsLocateError(errors.New("unrelated"))).To(BeFalse())
	})

	t.Run("IsRenderError predicate", func(t *testing.T) {
		g := NewWithT(t)

		re := &helm.RenderError{Chart: "x", ReleaseName: "y", Err: errors.New("bad yaml")}
		wrapped := fmt.Errorf("failed: %w", re)

		g.Expect(helm.IsRenderError(wrapped)).To(BeTrue())
		g.Expect(helm.IsRenderError(errors.New("unrelated"))).To(BeFalse())
	})

	t.Run("LocateError from invalid chart path via Process", func(t *testing.T) {
		g := NewWithT(t)

		renderer, err := helm.New([]helm.Source{
			{Chart: "/nonexistent/path/to/chart", ReleaseName: "test-release"},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = renderer.Process(t.Context(), nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(helm.IsLocateError(err)).To(BeTrue())

		var le *helm.LocateError
		g.Expect(errors.As(err, &le)).To(BeTrue())
		g.Expect(le.Chart).To(Equal("/nonexistent/path/to/chart"))
	})
}
