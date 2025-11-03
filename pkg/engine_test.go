package helm_test

import (
	"testing"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	. "github.com/onsi/gomega"
)

func TestNewEngine(t *testing.T) {

	t.Run("should create engine with Helm renderer", func(t *testing.T) {
		g := NewWithT(t)
		e, err := helm.NewEngine(helm.Source{
			Chart:       "oci://registry-1.docker.io/bitnamicharts/nginx",
			ReleaseName: "test-release",
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(e).ShouldNot(BeNil())
	})

	t.Run("should return error for invalid source", func(t *testing.T) {
		g := NewWithT(t)
		e, err := helm.NewEngine(helm.Source{
			Chart: "", // Missing chart
		})

		g.Expect(err).Should(HaveOccurred())
		g.Expect(e).Should(BeNil())
	})
}
