package helm

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

// Test requires helm and helm2 binaries in $PATH.
func Test_DetectHelmVersion(t *testing.T) {
	t.SkipNow()

	g := NewWithT(t)

	os.Setenv("HELM2", "yes")

	ver, err := DetectHelmVersion()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ver).Should(Equal("v2"))

	os.Setenv("HELM2", "")
	os.Setenv("HELM3", "yes")

	ver, err = DetectHelmVersion()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ver).Should(Equal("v3"))

	// Test autodetection
	os.Setenv("HELM2", "")
	os.Setenv("HELM3", "")

	ver, err = DetectHelmVersion()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ver).Should(Equal("v3"))

	os.Setenv("HELM_BIN_PATH", "helm2")
	ver, err = DetectHelmVersion()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ver).Should(Equal("v2"))
}
