package helm

import (
	"os"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v3/pkg/chart/loader"

	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	"github.com/flant/addon-operator/pkg/helm/nelm"
)

func TestHelmFactory(t *testing.T) {
	testCLient := func(t *testing.T, name string, clientType interface{}, envsToSet map[string]string) {
		g := NewWithT(t)

		for env, value := range envsToSet {
			os.Setenv(env, value)
			defer os.Unsetenv(env)
		}

		// For integration tests, but should be set before init
		namespace := os.Getenv("ADDON_OPERATOR_NAMESPACE")

		opts := &Options{
			Namespace: namespace,
			Logger:    log.NewNop(),
		}

		// Setup Helm client factory.
		helm, err := InitHelmClientFactory(opts, map[string]string{})
		g.Expect(err).ShouldNot(HaveOccurred())

		// Ensure client is of correct type
		helmCl := helm.NewClient(log.NewNop())
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

		chart, err := loader.Load("helm3lib/testdata/chart")
		g.Expect(err).ShouldNot(HaveOccurred())

		err = helmCl.UpgradeRelease("test-release", chart, nil, nil, nil, namespace)
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

	t.Run("init with helm3lib client", func(t *testing.T) {
		testCLient(t, "helm3", new(helm3lib.LibClient), map[string]string{})
	})

	t.Run("init with nelm client", func(t *testing.T) {
		testCLient(t, "nelm", new(nelm.NelmClient), map[string]string{"USE_NELM": "true"})
	})
}

func TestNelmSkipLogsAnnotation(t *testing.T) {
	g := NewWithT(t)

	// Test with nelm client type
	os.Setenv("USE_NELM", "true")
	defer os.Unsetenv("USE_NELM")

	opts := &Options{
		Namespace: "test-namespace",
		Logger:    log.NewNop(),
	}

	// Test that factory creates nelm client correctly
	helm, err := InitHelmClientFactory(opts, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(helm.ClientType).To(Equal(Nelm))

	// Test with existing labels - skip-logs should not be in labels
	existingLabels := map[string]string{
		"heritage": "addon-operator",
		"test":     "value",
	}
	helm2, err := InitHelmClientFactory(opts, existingLabels)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(helm2.labels).To(HaveKeyWithValue("heritage", "addon-operator"))
	g.Expect(helm2.labels).To(HaveKeyWithValue("test", "value"))
	g.Expect(helm2.labels).ToNot(HaveKey("werf.io/skip-logs"))
}

func TestHelm3LibNoSkipLogsAnnotation(t *testing.T) {
	g := NewWithT(t)

	// Test with helm3lib client type (default)
	os.Unsetenv("USE_NELM")

	opts := &Options{
		Namespace: "test-namespace",
		Logger:    log.NewNop(),
	}

	// Test that factory creates helm3lib client correctly
	helm, err := InitHelmClientFactory(opts, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(helm.ClientType).To(Equal(Helm3Lib))

	existingLabels := map[string]string{
		"heritage": "addon-operator",
		"test":     "value",
	}
	helm2, err := InitHelmClientFactory(opts, existingLabels)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(helm2.labels).ToNot(HaveKey("werf.io/skip-logs"))
	g.Expect(helm2.labels).To(HaveKeyWithValue("heritage", "addon-operator"))
	g.Expect(helm2.labels).To(HaveKeyWithValue("test", "value"))
}
