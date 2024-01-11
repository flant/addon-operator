package helm3lib

import (
	"io"
	"testing"

	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/flant/kube-client/fake"
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

	// We can not create labels, see comments in markReleaseWithOperatorLabel

	// namespaces, _ := cl.KubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	// nsExists := false
	// labelsExists := false
	// for _, ns := range namespaces.Items {
	// 	if ns.Name != "test-ns" {
	// 		continue
	// 	}
	// 	nsExists = true
	// 	labels := ns.GetLabels()
	// 	t.Logf("Labels: %v", labels)
	// 	value, ok := labels[app.ManagedResourceLabelKey]
	// 	if ok && value == app.ManagedResourceLabelValue {
	// 		labelsExists = true
	// 	}
	// 	break
	// }
	// g.Expect(nsExists).Should(BeTrue(), "namespace doesn't exists")
	// g.Expect(labelsExists).Should(BeTrue(), "namespace should contain operator labels")
}

func initHelmClient(t *testing.T) *LibClient {
	fCluster := fake.NewFakeCluster(fake.ClusterVersionV125)
	fCluster.CreateNs("test-ns")

	actionConfig = actionConfigFixture(t)

	cl := &LibClient{
		LogEntry:   log.NewEntry(log.StandardLogger()),
		KubeClient: fCluster.Client,
		Namespace:  "test-ns",
		HistoryMax: 10,
		Timeout:    0,
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
