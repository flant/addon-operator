package helm3lib

import (
	"io"
	"testing"

	"github.com/flant/kube-client/fake"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func TestHelm3LibEmptyCluster(t *testing.T) {
	g := NewWithT(t)

	cl := initHelmClient(t)

	releases, err := cl.ListReleasesNames(nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeComparableTo([]string{}), "should get empty list of releases")

	_, _, err = cl.LastReleaseStatus("some-release-name")
	g.Expect(err).Should(HaveOccurred(), "should fail getting release status in the empty cluster")

	isExists, err := cl.IsReleaseExists("some-release-name")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeFalse(), "should not found release in the empty cluster")
}

// TODO(future) use fake cluster to test helm actions.
func TestHelm3LibUpgradeDelete(t *testing.T) {
	g := NewWithT(t)

	cl := initHelmClient(t)

	err := cl.UpgradeRelease("test-release", "testdata/chart", nil, nil, cl.Namespace)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func initHelmClient(t *testing.T) *LibClient {
	g := NewWithT(t)

	fCluster := fake.NewFakeCluster(fake.ClusterVersionV125)

	err := Init(&Options{
		Namespace:  "test-ns",
		HistoryMax: 10,
		Timeout:    0,
		KubeClient: fCluster.Client,
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	actionConfig = actionConfigFixture(t)

	cl := &LibClient{
		LogEntry:   log.NewEntry(log.StandardLogger()),
		KubeClient: options.KubeClient,
		Namespace:  options.Namespace,
	}

	return cl
}

func actionConfigFixture(t *testing.T) *action.Configuration {
	t.Helper()

	registryClient, err := registry.NewClient()
	if err != nil {
		t.Fatal(err)
	}

	return &action.Configuration{
		Releases:       storage.Init(driver.NewMemory()),
		KubeClient:     &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}},
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
		Log: func(format string, v ...interface{}) {
			t.Helper()
			t.Logf(format, v...)
		},
	}
}
