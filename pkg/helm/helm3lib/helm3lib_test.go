package helm3lib

import (
	"io"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func TestHelm3LibEmptyCluster(t *testing.T) {
	g := NewWithT(t)

	cl := initHelmClient(t)

	releases, err := cl.ListReleasesNames()
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

	releases, err := cl.ListReleasesNames()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeComparableTo([]string{"test-release"}), "should get list of releases")

	revision, status, err := cl.LastReleaseStatus("test-release")
	g.Expect(err).ShouldNot(HaveOccurred(), "should get release status in the cluster")
	g.Expect(status).To(Equal("deployed"), "status of the release should be deployed")
	g.Expect(revision).To(Equal("1"), "revision of the release should be 1")

	isExists, err := cl.IsReleaseExists("test-release")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeTrue(), "should found the release in the cluster")

	err = cl.DeleteRelease("test-release")
	g.Expect(err).ShouldNot(HaveOccurred())

	isExists, err = cl.IsReleaseExists("test-release")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeFalse(), "should not found the release in the cluster")
}

func initHelmClient(t *testing.T) *LibClient {
	Init(&Options{
		Namespace:  "test-ns",
		HistoryMax: 10,
		Timeout:    0,
	}, log.NewNop())

	actionConfig = actionConfigFixture(t)

	cl := &LibClient{
		Logger:    log.NewNop(),
		Namespace: options.Namespace,
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

// BenchmarkRESTMapper is here to remember that helm does not cache the client by default.
func BenchmarkRESTMapper(b *testing.B) {
	ns := "test"

	getterEnv := cli.New().RESTClientGetter()
	getterPersistent := buildConfigFlagsFromEnv(&ns, cli.New())

	b.Run("Env client", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = getterEnv.ToRESTMapper()
		}
	})

	b.Run("Persistent client", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = getterPersistent.ToRESTMapper()
		}
	})
}
