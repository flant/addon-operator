package helm

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	klient "github.com/flant/kube-client/client"
)

func TestHelmFactory(t *testing.T) {
	t.Run("init with helm3 binary client", func(t *testing.T) {
		g := NewWithT(t)

		// Explicitly set path to the Helm3 binary.
		_ = os.Setenv("HELM_BIN_PATH", "testdata/helm-fake/helm3/helm")
		defer os.Unsetenv("HELM_BIN_PATH")

		// Setup fake client.
		kubeClient := klient.NewFake(nil)

		// Setup Helm client factory.
		helm, err := InitHelmClientFactory(kubeClient)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Ensure client is a builtin Helm3 library.
		helmCl := helm.NewClient("default", nil)
		g.Expect(helmCl).To(BeAssignableToTypeOf(new(helm3.Helm3Client)), "should create helm3ib client")
	})

	t.Run("init with helm3lib client", func(t *testing.T) {
		g := NewWithT(t)

		// Explicitly use builtin Helm client.
		_ = os.Setenv("HELM3LIB", "yes")
		defer os.Unsetenv("HELM3LIB")

		// Setup fake client.
		kubeClient := klient.NewFake(nil)

		// Setup Helm client factory.
		helm, err := InitHelmClientFactory(kubeClient)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Ensure client is a builtin Helm3 library.
		helmCl := helm.NewClient("default", nil)
		g.Expect(helmCl).To(BeAssignableToTypeOf(new(helm3lib.LibClient)), "should create helm3ib client")

		// Do simple test against fake cluster.
		var releases []string

		releases, err = helmCl.ListReleasesNames(nil)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(releases).To(BeComparableTo([]string{}), "should get empty list of releases")
	})
}
