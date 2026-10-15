package helm

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_DetectHelmVersion(t *testing.T) {
	t.Run("default return Helm3Lib", func(t *testing.T) {
		g := NewWithT(t)

		ver, err := DetectHelmVersion()
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ver).Should(BeEquivalentTo(Helm3Lib))
	})

	t.Run("return Nelm when USE_NELM is set", func(t *testing.T) {
		g := NewWithT(t)

		t.Setenv("USE_NELM", "true")
		defer t.Setenv("USE_NELM", "")

		ver, err := DetectHelmVersion()
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(ver).Should(BeEquivalentTo(Nelm))
	})

	t.Run("return error if helm2 env is set", func(t *testing.T) {
		g := NewWithT(t)

		// Set one of the helm2 envs
		t.Setenv("HELM2", "true")
		defer t.Setenv("HELM2", "")

		ver, err := DetectHelmVersion()
		g.Expect(err).Should(HaveOccurred())
		g.Expect(ver).Should(BeEmpty())
	})
}

func Test_DetectHelmVersion_Helm2(t *testing.T) {
	t.Run("tiller settings are present", func(t *testing.T) {
		g := NewWithT(t)

		_ = os.Setenv("TILLER_MAX_HISTORY", "10")
		defer os.Unsetenv("TILLER_MAX_HISTORY")

		ver, err := DetectHelmVersion()
		g.Expect(ver).Should(BeEquivalentTo(""))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring(helm2DeprecationMsg))
	})

	t.Run("explicit helm2 use", func(t *testing.T) {
		g := NewWithT(t)

		_ = os.Setenv("HELM2", "yes")
		defer os.Unsetenv("HELM2")

		ver, err := DetectHelmVersion()
		g.Expect(ver).Should(BeEquivalentTo(""))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring(helm2DeprecationMsg))
	})
}
