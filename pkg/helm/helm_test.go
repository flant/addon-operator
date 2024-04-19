package helm

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

func TestHelmFactory(t *testing.T) {
	testCLient := func(t *testing.T, name string, clientType interface{}, envsToSet map[string]string) {
		g := NewWithT(t)

		for env, value := range envsToSet {
			os.Setenv(env, value)
			defer os.Unsetenv(env)
		}

		// For integration tests, but should be set before init
		app.Namespace = os.Getenv("ADDON_OPERATOR_NAMESPACE")
		// Setup Helm client factory.
		helm, err := InitHelmClientFactory()
		g.Expect(err).ShouldNot(HaveOccurred())

		// Ensure client is a builtin Helm3 library.
		helmCl := helm.NewClient(nil)
		g.Expect(helmCl).To(BeAssignableToTypeOf(clientType), "should create %s client", name)

		if os.Getenv("ADDON_OPERATOR_HELM_INTEGRATION_TEST") != "yes" {
			t.Skip("Do not run this on CI")
		}

		releases, err := helmCl.ListReleasesNames()
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(releases).To(BeComparableTo([]string{}), "should get empty list of releases")

		_, _, err = helmCl.LastReleaseStatus("test-release")
		g.Expect(err).Should(HaveOccurred(), "should fail getting release status in the empty cluster")

		isExists, err := helmCl.IsReleaseExists("test-release")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(isExists).Should(BeFalse(), "should not found release in the empty cluster")

		err = helmCl.UpgradeRelease("test-release", "helm3lib/testdata/chart", nil, nil, app.Namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		releasesAfterUpgrade, err := helmCl.ListReleasesNames()
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(releasesAfterUpgrade).To(BeComparableTo([]string{"test-release"}), "should get list of releases")

		revision, status, err := helmCl.LastReleaseStatus("test-release")
		g.Expect(err).ShouldNot(HaveOccurred(), "should get release status in the cluster")
		g.Expect(status).To(Equal("deployed"), "status of the release should be deployed")
		g.Expect(revision).To(Equal("1"), "revision of the release should be 1")

		isExistsAfterUpgrade, err := helmCl.IsReleaseExists("test-release")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(isExistsAfterUpgrade).Should(BeTrue(), "should found the release in the cluster")

		err = helmCl.DeleteRelease("test-release")
		g.Expect(err).ShouldNot(HaveOccurred())

		isExistsAfterDelete, err := helmCl.IsReleaseExists("test-release")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(isExistsAfterDelete).Should(BeFalse(), "should not found the release in the cluster")
	}

	t.Run("init with helm3 binary client", func(t *testing.T) {
		testCLient(t, "helm3lib", new(helm3.Helm3Client), map[string]string{"HELM_BIN_PATH": "/opt/homebrew/bin/helm"})
	})

	t.Run("init with helm3lib client", func(t *testing.T) {
		testCLient(t, "helm3", new(helm3lib.LibClient), map[string]string{"HELM3LIB": "yes"})
	})
}
