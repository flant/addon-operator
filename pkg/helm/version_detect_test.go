package helm

import (
	"os"
	"path/filepath"
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

func alterPATH(newPath string) func() {
	origPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", newPath+string(os.PathListSeparator)+origPath)
	return func() {
		_ = os.Setenv("PATH", origPath)
	}
}

func toAbsolutePath(path string) string {
	abs, _ := filepath.Abs(path)
	return abs
}
