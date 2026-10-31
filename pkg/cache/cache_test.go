package cache_test

import (
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/chart/common"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	utilcache "github.com/k8s-manifest-kit/pkg/util/cache"
	"github.com/k8s-manifest-kit/renderer-helm/pkg/cache"

	. "github.com/onsi/gomega"
)

func testObjects(name string) []unstructured.Unstructured {
	return []unstructured.Unstructured{
		{Object: map[string]any{
			"kind": "ConfigMap",
			"metadata": map[string]any{
				"name": name,
			},
		}},
	}
}

func TestNew(t *testing.T) {
	t.Run("should disable caching for nil options", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(cache.New(nil)).To(BeNil())
	})

	t.Run("should key default entries by the exported spec", func(t *testing.T) {
		g := NewWithT(t)

		c := cache.New(&utilcache.Options{})

		spec := cache.ChartSpec{
			Chart:       "./charts/app",
			ReleaseName: "release",
			Values:      common.Values{"replicaCount": 1},
		}

		c.Set(spec, testObjects("default-key"))

		cached, found := c.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(cached).To(HaveLen(1))
		g.Expect(cached[0].GetName()).To(Equal("default-key"))

		// Different values produce a different default key.
		other := spec
		other.Values = common.Values{"replicaCount": 2}
		_, found = c.Get(other)
		g.Expect(found).To(BeFalse())
	})

	t.Run("should preserve custom key functions", func(t *testing.T) {
		g := NewWithT(t)

		c := cache.New(&utilcache.Options{
			KeyFunc: cache.FastCacheKeyFunc,
		})

		spec := cache.ChartSpec{
			Chart:       "./charts/app",
			ReleaseName: "release",
			Values:      common.Values{"replicaCount": 1},
		}

		c.Set(spec, testObjects("custom-key"))

		// FastCacheKeyFunc ignores values, so different values still hit.
		other := spec
		other.Values = common.Values{"replicaCount": 2}

		cached, found := c.Get(other)
		g.Expect(found).To(BeTrue())
		g.Expect(cached[0].GetName()).To(Equal("custom-key"))
	})

	t.Run("should honor the configured TTL", func(t *testing.T) {
		g := NewWithT(t)

		c := cache.New(&utilcache.Options{TTL: 100 * time.Millisecond})

		spec := cache.ChartSpec{Chart: "./charts/app"}

		c.Set(spec, testObjects("expiring"))

		cached, found := c.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(cached[0].GetName()).To(Equal("expiring"))

		time.Sleep(150 * time.Millisecond)

		_, found = c.Get(spec)
		g.Expect(found).To(BeFalse())
	})

	t.Run("should not mutate the caller options", func(t *testing.T) {
		g := NewWithT(t)

		opts := &utilcache.Options{TTL: time.Minute}

		cache.New(opts)

		// The default key function injection must not leak into the caller's
		// options, and the TTL must be preserved as configured.
		g.Expect(opts.KeyFunc).To(BeNil())
		g.Expect(opts.TTL).To(Equal(time.Minute))
	})

	t.Run("should not share entries between instances", func(t *testing.T) {
		g := NewWithT(t)

		first := cache.New(&utilcache.Options{})
		second := cache.New(&utilcache.Options{})

		spec := cache.ChartSpec{Chart: "./charts/app"}

		first.Set(spec, testObjects("first"))

		_, found := second.Get(spec)
		g.Expect(found).To(BeFalse())

		second.Set(spec, testObjects("second"))

		cached, found := first.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(cached[0].GetName()).To(Equal("first"))

		cached, found = second.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(cached[0].GetName()).To(Equal("second"))
	})

	t.Run("should deep clone stored and returned objects", func(t *testing.T) {
		g := NewWithT(t)

		c := cache.New(&utilcache.Options{})

		spec := cache.ChartSpec{Chart: "./charts/app"}

		stored := testObjects("cloned")
		c.Set(spec, stored)

		// Mutating the caller's slice after Set must not affect the cache.
		stored[0].SetName("mutated-stored")

		first, found := c.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(first[0].GetName()).To(Equal("cloned"))

		// Mutating a returned slice must not affect later retrievals.
		first[0].SetName("mutated-returned")

		second, found := c.Get(spec)
		g.Expect(found).To(BeTrue())
		g.Expect(second[0].GetName()).To(Equal("cloned"))
	})
}

func TestFastCacheKeyFunc(t *testing.T) {
	t.Run("should ignore values", func(t *testing.T) {
		g := NewWithT(t)

		base := cache.ChartSpec{
			Chart:          "./charts/app",
			ReleaseName:    "release",
			ReleaseVersion: "1.0.0",
			Values:         common.Values{"replicaCount": 1},
		}

		changedValues := base
		changedValues.Values = common.Values{"replicaCount": 99}

		g.Expect(cache.FastCacheKeyFunc(changedValues)).To(Equal(cache.FastCacheKeyFunc(base)))
	})

	t.Run("should distinguish chart, release, and version", func(t *testing.T) {
		g := NewWithT(t)

		base := cache.ChartSpec{
			Chart:          "./charts/app",
			ReleaseName:    "release",
			ReleaseVersion: "1.0.0",
		}

		otherChart := base
		otherChart.Chart = "./charts/other"

		otherRelease := base
		otherRelease.ReleaseName = "other-release"

		otherVersion := base
		otherVersion.ReleaseVersion = "2.0.0"

		baseKey := cache.FastCacheKeyFunc(base)

		g.Expect(cache.FastCacheKeyFunc(otherChart)).ToNot(Equal(baseKey))
		g.Expect(cache.FastCacheKeyFunc(otherRelease)).ToNot(Equal(baseKey))
		g.Expect(cache.FastCacheKeyFunc(otherVersion)).ToNot(Equal(baseKey))
	})

	t.Run("should return an empty string for non-spec inputs", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(cache.FastCacheKeyFunc("not-a-spec")).To(Equal(""))
		g.Expect(cache.FastCacheKeyFunc(42)).To(Equal(""))
		g.Expect(cache.FastCacheKeyFunc(nil)).To(Equal(""))
	})
}
