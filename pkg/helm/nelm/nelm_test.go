package nelm

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/common"
	nelmLog "github.com/werf/nelm/pkg/log"
	"github.com/werf/nelm/pkg/resource/spec"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg"
)

func Test_NewNelmClient(t *testing.T) {
	InitDefaultLogger(log.NewNop())
	singleLogger := nelmLog.Default

	cl := NewNelmClient(&CommonOptions{}, log.NewNop().Named("nelm"), map[string]string{})
	assert.NotNil(t, cl)
	assert.Equal(t, singleLogger, nelmLog.Default, "NewNelmClient must not override the global nelm logger")

	InitDefaultLogger(log.NewNop())
	assert.Equal(t, singleLogger, nelmLog.Default, "InitDefaultLogger must set the global nelm logger only once")
}

func Test_NelmLogger_ModuleFromContext(t *testing.T) {
	buf := &bytes.Buffer{}
	nl := newNelmLogger(log.NewLogger(log.WithOutput(buf)))

	nl.Info(contextWithModule(context.Background(), "node-manager"), "progress for %s", "node-manager")

	var entry map[string]any
	assert.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "node-manager", entry[pkg.LogKeyModule])
	assert.Equal(t, "progress for node-manager", entry["msg"])

	buf.Reset()
	nl.Info(context.Background(), "no module here")

	var neutral map[string]any
	assert.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &neutral))
	_, hasModule := neutral[pkg.LogKeyModule]
	assert.False(t, hasModule, "module must be absent when context has no module")
}

func Test_NelmClient_Render_KeepsOnlyRegularResources(t *testing.T) {
	cl := NewNelmClient(&CommonOptions{}, log.NewNop().Named("nelm"), nil)
	cl.actions = &fakeNelmActions{
		chartRenderResult: &action.ChartRenderResultV2{
			Resources: []*spec.ResourceSpec{
				renderedResource("apps/v1", "Deployment", "regular-deployment", common.StoreAsRegular),
				renderedResource("batch/v1", "Job", "pre-delete-hook", common.StoreAsHook),
				renderedResource("apps/v1", "Deployment", "another-regular-deployment", common.StoreAsRegular),
				renderedResource("apiextensions.k8s.io/v1", "CustomResourceDefinition", "standalone-crd", common.StoreAsNone),
			},
		},
	}

	rendered, err := cl.Render("test-release", "/some/chart", nil, nil, nil, "test-ns", false)
	assert.NoError(t, err)

	assert.Contains(t, rendered, "regular-deployment")
	assert.Contains(t, rendered, "another-regular-deployment")
	assert.NotContains(t, rendered, "pre-delete-hook", "helm hooks must not be rendered")
	assert.NotContains(t, rendered, "standalone-crd", "standalone CRDs must not be rendered")
	assert.Equal(t, 1, strings.Count(rendered, "---"), "two regular resources must be joined by a single separator")
}

func renderedResource(apiVersion, kind, name string, storeAs common.StoreAs) *spec.ResourceSpec {
	return &spec.ResourceSpec{
		Unstruct: &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   map[string]any{"name": name},
		}},
		StoreAs: storeAs,
	}
}

// fakeNelmActions stubs nelm calls in tests; only ChartRender returns data.
type fakeNelmActions struct {
	chartRenderResult *action.ChartRenderResultV2
}

func (f *fakeNelmActions) ReleaseGet(_ context.Context, _, _ string, _ action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error) {
	return nil, nil
}

func (f *fakeNelmActions) ReleaseInstall(_ context.Context, _, _ string, _ action.ReleaseInstallOptions) error {
	return nil
}

func (f *fakeNelmActions) ReleaseUninstall(_ context.Context, _, _ string, _ action.ReleaseUninstallOptions) error {
	return nil
}

func (f *fakeNelmActions) ReleaseList(_ context.Context, _ action.ReleaseListOptions) (*action.ReleaseListResultV1, error) {
	return nil, nil
}

func (f *fakeNelmActions) ChartRender(_ context.Context, _ action.ChartRenderOptions) (*action.ChartRenderResultV2, error) {
	return f.chartRenderResult, nil
}
