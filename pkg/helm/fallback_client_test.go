package helm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	. "github.com/onsi/gomega"
	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/resource"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/utils"
)

// fakeMetricStorage captures CounterAdd calls for assertions.
type fakeMetricStorage struct {
	mu      sync.Mutex
	counter []counterCall
}

type counterCall struct {
	metric string
	value  float64
	labels map[string]string
}

func (m *fakeMetricStorage) CounterAdd(metric string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clone := make(map[string]string, len(labels))
	for k, v := range labels {
		clone[k] = v
	}

	m.counter = append(m.counter, counterCall{metric: metric, value: value, labels: clone})
}

func (m *fakeMetricStorage) calls() []counterCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]counterCall, len(m.counter))
	copy(out, m.counter)

	return out
}

// fakeHelmClient is a minimal client.HelmClient used to exercise FallbackClient.
type fakeHelmClient struct {
	name string

	upgradeErr error
	renderErr  error
	deleteErr  error

	upgradeCalls int
	renderCalls  int
	deleteCalls  int
	lastStatus   int
	logLabels    map[string]string
	extraLabels  map[string]string
}

var _ client.HelmClient = (*fakeHelmClient)(nil)

func (c *fakeHelmClient) UpgradeRelease(_, _ string, _, _ []string, _ map[string]string, _ string) error {
	c.upgradeCalls++
	return c.upgradeErr
}

func (c *fakeHelmClient) Render(_, _ string, _, _ []string, _ map[string]string, _ string, _ bool) (string, error) {
	c.renderCalls++
	if c.renderErr != nil {
		return "", c.renderErr
	}
	return c.name, nil
}

func (c *fakeHelmClient) DeleteRelease(_ string) error {
	c.deleteCalls++
	return c.deleteErr
}

func (c *fakeHelmClient) LastReleaseStatus(_ string) (string, string, error) {
	c.lastStatus++
	return "1", "deployed", nil
}

func (c *fakeHelmClient) GetReleaseValues(_ string) (utils.Values, error) {
	return utils.Values{}, nil
}

func (c *fakeHelmClient) GetReleaseChecksum(_ string) (string, error) {
	return "", nil
}

func (c *fakeHelmClient) GetReleaseLabels(_, _ string) (string, error) {
	return "", nil
}

func (c *fakeHelmClient) ListReleasesNames() ([]string, error) {
	return nil, nil
}

func (c *fakeHelmClient) IsReleaseExists(_ string) (bool, error) {
	return false, nil
}

func (c *fakeHelmClient) WithLogLabels(labels map[string]string) {
	c.logLabels = labels
}

func (c *fakeHelmClient) WithExtraLabels(labels map[string]string) {
	c.extraLabels = labels
}

// chartDir writes a single-template chart and returns its path. The fallback path scans the
// chart on disk, so tests must point at a real directory laid out the way helm expects.
func chartDir(t *testing.T, template string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o700); err != nil {
		t.Fatalf("create chart templates dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "templates", "manifest.yaml"), []byte(template), 0o600); err != nil {
		t.Fatalf("write chart template: %v", err)
	}

	return dir
}

// writeHook drops a file into the chart's hooks directory, standing in for a module's
// compiled hook binary.
func writeHook(t *testing.T, chartPath, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(chartPath, "hooks"), 0o700); err != nil {
		t.Fatalf("create chart hooks dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(chartPath, "hooks", "hook"), []byte(content), 0o700); err != nil {
		t.Fatalf("write chart hook: %v", err)
	}
}

const (
	plainTemplate = "kind: Deployment\nmetadata:\n  name: test\n"
	werfTemplate  = "kind: Deployment\nmetadata:\n  name: test\n  annotations:\n    werf.io/weight: \"-1\"\n"
)

func TestChartUsesWerfAnnotations(t *testing.T) {
	g := NewWithT(t)

	uses, err := chartUsesWerfAnnotations(chartDir(t, werfTemplate))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(uses).To(BeTrue())

	uses, err = chartUsesWerfAnnotations(chartDir(t, plainTemplate))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(uses).To(BeFalse())

	// A hook binary can carry the annotation string in compiled code without the chart ever
	// declaring it, so hooks must not be scanned.
	withHook := chartDir(t, plainTemplate)
	writeHook(t, withHook, "\x7fELF...werf.io/weight...")
	uses, err = chartUsesWerfAnnotations(withHook)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(uses).To(BeFalse())

	_, err = chartUsesWerfAnnotations(filepath.Join(t.TempDir(), "missing"))
	g.Expect(err).Should(HaveOccurred())
}

func TestShouldFallback(t *testing.T) {
	g := NewWithT(t)

	g.Expect(shouldFallback(nil)).To(BeFalse())
	g.Expect(shouldFallback(errors.New("random"))).To(BeTrue())

	g.Expect(shouldFallback(action.ErrBuildPlan)).To(BeTrue())
	g.Expect(shouldFallback(resource.ErrResourceDuplicatesFound)).To(BeTrue())

	g.Expect(shouldFallback(fmt.Errorf("install nelm release: %w", action.ErrBuildPlan))).To(BeTrue())
	g.Expect(shouldFallback(fmt.Errorf("validate: %w: details", resource.ErrResourceDuplicatesFound))).To(BeTrue())
}

func TestFallbackClient_UpgradeRelease(t *testing.T) {
	t.Run("primary success, no fallback", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{name: "primary"}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("r", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(0))
		g.Expect(ms.calls()).To(BeEmpty())
	})

	t.Run("any primary error triggers fallback", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{name: "primary", upgradeErr: errors.New("boom")}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("r", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(1))
		g.Expect(ms.calls()).To(HaveLen(1))
	})

	t.Run("falls back on action.ErrBuildPlan", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("install: %w", action.ErrBuildPlan),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("my-release", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(1))

		// the telemetry mirror is emitted alongside, with the same value and labels
		calls := ms.calls()
		g.Expect(calls).To(HaveLen(2))
		g.Expect([]string{calls[0].metric, calls[1].metric}).To(ConsistOf(
			metrics.HelmFallbackTotal, metrics.HelmFallbackTelemetryTotal))

		for _, call := range calls {
			g.Expect(call.value).To(Equal(1.0))
			g.Expect(call.labels).To(HaveKeyWithValue(pkg.MetricKeyModule, "my-release"))
			g.Expect(call.labels).To(HaveKeyWithValue(pkg.MetricKeyOperation, "upgrade"))
			g.Expect(call.labels).To(HaveKeyWithValue(pkg.MetricKeyErrorType, "build_plan"))
		}
	})

	t.Run("falls back on resource.ErrResourceDuplicatesFound", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("validate: %w", resource.ErrResourceDuplicatesFound),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("my-release", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(1))

		calls := ms.calls()
		g.Expect(calls).To(HaveLen(2))

		for _, call := range calls {
			g.Expect(call.labels).To(HaveKeyWithValue(pkg.MetricKeyErrorType, "resource_duplicates"))
			g.Expect(call.labels).To(HaveKeyWithValue(pkg.MetricKeyOperation, "upgrade"))
		}
	})

	t.Run("chart with werf annotations is never handed to helm", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("install: %w", action.ErrBuildPlan),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("my-release", chartDir(t, werfTemplate), nil, nil, nil, "ns")
		g.Expect(err).Should(MatchError(action.ErrBuildPlan))
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(0))
		g.Expect(ms.calls()).To(BeEmpty())
	})

	t.Run("unscannable chart is never handed to helm", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("install: %w", action.ErrBuildPlan),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("my-release", filepath.Join(t.TempDir(), "missing"), nil, nil, nil, "ns")
		g.Expect(err).Should(MatchError(action.ErrBuildPlan))
		g.Expect(fallback.upgradeCalls).To(Equal(0))
		g.Expect(ms.calls()).To(BeEmpty())
	})

	t.Run("fallback error is propagated and still counted", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("install: %w", action.ErrBuildPlan),
		}
		fallback := &fakeHelmClient{name: "fallback", upgradeErr: errors.New("fallback failed")}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		err := c.UpgradeRelease("r", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).Should(MatchError("fallback failed"))
		g.Expect(primary.upgradeCalls).To(Equal(1))
		g.Expect(fallback.upgradeCalls).To(Equal(1))
		g.Expect(ms.calls()).To(HaveLen(2))
	})

	t.Run("nil metric storage is safe", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:       "primary",
			upgradeErr: fmt.Errorf("install: %w", action.ErrBuildPlan),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		c := NewFallbackClient(primary, fallback, log.NewNop(), nil)

		err := c.UpgradeRelease("r", chartDir(t, plainTemplate), nil, nil, nil, "ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(fallback.upgradeCalls).To(Equal(1))
	})
}

func TestFallbackClient_Render(t *testing.T) {
	t.Run("primary success returns primary output", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{name: "primary"}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		out, err := c.Render("r", "c", nil, nil, nil, "ns", false)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(out).To(Equal("primary"))
		g.Expect(fallback.renderCalls).To(Equal(0))
		g.Expect(ms.calls()).To(BeEmpty())
	})

	t.Run("falls back on recoverable error and records metric", func(t *testing.T) {
		g := NewWithT(t)
		primary := &fakeHelmClient{
			name:      "primary",
			renderErr: fmt.Errorf("validate: %w", resource.ErrResourceDuplicatesFound),
		}
		fallback := &fakeHelmClient{name: "fallback"}
		ms := &fakeMetricStorage{}
		c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

		out, err := c.Render("r", "c", nil, nil, nil, "ns", false)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(out).To(Equal("fallback"))

		calls := ms.calls()
		g.Expect(calls).To(HaveLen(2))
		g.Expect(calls[0].labels).To(HaveKeyWithValue(pkg.MetricKeyOperation, "render"))
		g.Expect(calls[0].labels).To(HaveKeyWithValue(pkg.MetricKeyErrorType, "resource_duplicates"))
	})
}

func TestFallbackClient_DeleteRelease(t *testing.T) {
	g := NewWithT(t)
	primary := &fakeHelmClient{
		name:      "primary",
		deleteErr: fmt.Errorf("delete: %w", action.ErrBuildPlan),
	}
	fallback := &fakeHelmClient{name: "fallback"}
	ms := &fakeMetricStorage{}
	c := NewFallbackClient(primary, fallback, log.NewNop(), ms)

	err := c.DeleteRelease("r")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(primary.deleteCalls).To(Equal(1))
	g.Expect(fallback.deleteCalls).To(Equal(1))

	calls := ms.calls()
	g.Expect(calls).To(HaveLen(2))
	g.Expect(calls[0].labels).To(HaveKeyWithValue(pkg.MetricKeyOperation, "delete"))
	g.Expect(calls[0].labels).To(HaveKeyWithValue(pkg.MetricKeyErrorType, "build_plan"))
}

func TestFallbackClient_LabelPropagation(t *testing.T) {
	g := NewWithT(t)
	primary := &fakeHelmClient{name: "primary"}
	fallback := &fakeHelmClient{name: "fallback"}
	c := NewFallbackClient(primary, fallback, log.NewNop(), nil)

	c.WithExtraLabels(map[string]string{"foo": "bar"})
	c.WithLogLabels(map[string]string{"req": "1"})

	g.Expect(primary.extraLabels).To(HaveKeyWithValue("foo", "bar"))
	g.Expect(fallback.extraLabels).To(HaveKeyWithValue("foo", "bar"))
	g.Expect(primary.logLabels).To(HaveKeyWithValue("req", "1"))
	g.Expect(fallback.logLabels).To(HaveKeyWithValue("req", "1"))
}

func TestFallbackClient_ReadOnlyMethodsUsePrimary(t *testing.T) {
	g := NewWithT(t)
	primary := &fakeHelmClient{name: "primary"}
	fallback := &fakeHelmClient{name: "fallback"}
	c := NewFallbackClient(primary, fallback, log.NewNop(), nil)

	_, _, _ = c.LastReleaseStatus("r")
	g.Expect(primary.lastStatus).To(Equal(1))
	g.Expect(fallback.lastStatus).To(Equal(0))
}
