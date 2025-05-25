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
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func TestHelm3LibEmptyCluster(t *testing.T) {
	g := NewWithT(t)

	cl := initHelmClient(t)

	releasesNames, err := cl.ListReleasesNames()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releasesNames).To(BeComparableTo([]string{}), "should get empty list of releases")

	_, _, err = cl.LastReleaseStatus("some-release-name")
	g.Expect(err).Should(HaveOccurred(), "should fail getting release status in the empty cluster")

	isExists, err := cl.IsReleaseExists("some-release-name")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeFalse(), "should not found release in the empty cluster")

	releaseList, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releaseList).To(BeEmpty(), "should get empty list of releases")
}

// TODO(future) use fake cluster to test helm actions.
func TestHelm3LibUpgradeDelete(t *testing.T) {
	g := NewWithT(t)

	cl := initHelmClient(t)

	releases, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeEmpty(), "should get empty list of releases")

	err = cl.UpgradeRelease("test-release", "testdata/chart", nil, nil, map[string]string{"key": "value"}, cl.Namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	releasesNames, err := cl.ListReleasesNames()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releasesNames).To(BeComparableTo([]string{"test-release"}), "should get list of releases")

	releaseList, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releaseList).To(HaveLen(1), "should get list of releases")
	g.Expect(releaseList[0].Name).To(Equal("test-release"), "should get list of releases")
	g.Expect(releaseList[0].Namespace).To(Equal(cl.Namespace), "should get list of releases")
	g.Expect(releaseList[0].Info.Status).To(Equal(release.StatusDeployed), "should get list of releases")
	g.Expect(releaseList[0].Labels).To(Equal(map[string]string{"key": "value"}), "should get list of releases")

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

	releaseList, err = cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releaseList).To(BeEmpty(), "should get empty list of releases after delete")
}

func TestReleaseExistsReturnsTrueForExistingRelease(t *testing.T) {
	g := NewWithT(t)
	cl := initHelmClient(t)

	releases, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeEmpty(), "should get empty list of releases")

	err = cl.UpgradeRelease("existing-release", "testdata/chart", nil, nil, nil, cl.Namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	releases, err = cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(HaveLen(1), "should have one release in the list")
	g.Expect(releases[0].Name).To(Equal("existing-release"), "should have the correct release name")
	g.Expect(releases[0].Namespace).To(Equal(cl.Namespace), "should have the correct release namespace")
	g.Expect(releases[0].Info.Status).To(Equal(release.StatusDeployed), "should have the correct release status")
	g.Expect(releases[0].Labels).To(BeEmpty(), "should have no labels for the release")
	g.Expect(releases[0].Chart.Metadata.Name).To(Equal("hello"), "should have the correct chart name")

	isExists, err := cl.IsReleaseExists("existing-release")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeTrue(), "should return true for an existing release")
}

func TestReleaseExistsReturnsFalseForNonExistingRelease(t *testing.T) {
	g := NewWithT(t)
	cl := initHelmClient(t)

	isExists, err := cl.IsReleaseExists("non-existing-release")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeFalse(), "should return false for a non-existing release")

	releases, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeEmpty(), "should get empty list of releases")
}

func TestDeleteReleaseRemovesExistingRelease(t *testing.T) {
	g := NewWithT(t)
	cl := initHelmClient(t)

	releases, err := cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeEmpty(), "should get empty list of releases")

	err = cl.UpgradeRelease("release-to-delete", "testdata/chart", nil, nil, nil, cl.Namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	releases, err = cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(HaveLen(1), "should have one release in the list")
	g.Expect(releases[0].Name).To(Equal("release-to-delete"), "should have the correct release name")
	g.Expect(releases[0].Namespace).To(Equal(cl.Namespace), "should have the correct release namespace")
	g.Expect(releases[0].Info.Status).To(Equal(release.StatusDeployed), "should have the correct release status")
	g.Expect(releases[0].Labels).To(BeEmpty(), "should have no labels for the release")
	g.Expect(releases[0].Chart.Metadata.Name).To(Equal("hello"), "should have the correct chart name")

	err = cl.DeleteRelease("release-to-delete")
	g.Expect(err).ShouldNot(HaveOccurred())

	releases, err = cl.ListReleases()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(releases).To(BeEmpty(), "should get empty list of releases after deletion")

	isExists, err := cl.IsReleaseExists("release-to-delete")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(isExists).Should(BeFalse(), "should not find the release after deletion")
}

func TestLastReleaseStatusReturnsCorrectStatusForExistingRelease(t *testing.T) {
	g := NewWithT(t)
	cl := initHelmClient(t)

	err := cl.UpgradeRelease("status-release", "testdata/chart", nil, nil, nil, cl.Namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	revision, status, err := cl.LastReleaseStatus("status-release")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(revision).To(Equal("1"), "should return correct revision for the release")
	g.Expect(status).To(Equal("deployed"), "should return correct status for the release")
}

func TestLastReleaseStatusReturnsErrorForNonExistingRelease(t *testing.T) {
	g := NewWithT(t)
	cl := initHelmClient(t)

	_, _, err := cl.LastReleaseStatus("non-existing-release")
	g.Expect(err).Should(HaveOccurred(), "should return an error for a non-existing release")
}

func TestParseSetValues(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name     string
		input    []string
		expected map[string]any
	}{
		{
			name:     "empty input returns empty map",
			input:    nil,
			expected: map[string]any{},
		},
		{
			name:     "empty slice returns empty map",
			input:    []string{},
			expected: map[string]any{},
		},
		{
			name:     "single key-value",
			input:    []string{"foo=bar"},
			expected: map[string]any{"foo": "bar"},
		},
		{
			name:     "multiple key-values",
			input:    []string{"foo=bar", "baz=qux"},
			expected: map[string]any{"foo": "bar", "baz": "qux"},
		},
		{
			name:     "value with equals sign",
			input:    []string{"foo=bar=baz"},
			expected: map[string]any{"foo": "bar=baz"},
		},
		{
			name:     "key and value with whitespace",
			input:    []string{" foo = bar "},
			expected: map[string]any{"foo": "bar"},
		},
		{
			name:     "entry with no equals is skipped",
			input:    []string{"foo"},
			expected: map[string]any{},
		},
		{
			name:     "entry with empty key is skipped",
			input:    []string{"=bar"},
			expected: map[string]any{},
		},
		{
			name:     "entry with empty value",
			input:    []string{"foo="},
			expected: map[string]any{"foo": ""},
		},
		{
			name:     "duplicate keys, last wins",
			input:    []string{"foo=bar", "foo=baz"},
			expected: map[string]any{"foo": "baz"},
		},
		{
			name:     "mixed valid and invalid entries",
			input:    []string{"foo=bar", "baz", "=qux", "key=value=with=equals", "  spaced = spaced value  "},
			expected: map[string]any{"foo": "bar", "key": "value=with=equals", "spaced": "spaced value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			result := parseSetValues(tt.input)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func initHelmClient(t *testing.T) *LibClient {
	g := NewWithT(t)
	err := Init(&Options{
		Namespace:  "test-ns",
		HistoryMax: 10,
		Timeout:    0,
	}, log.NewNop())
	g.Expect(err).NotTo(HaveOccurred())

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
