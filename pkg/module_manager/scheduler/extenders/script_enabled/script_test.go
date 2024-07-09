package script_enabled

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	exerror "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/error"
	node_mock "github.com/flant/addon-operator/pkg/module_manager/scheduler/node/mock"
)

func TestExtender(t *testing.T) {
	var boolNilP *bool
	tmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)

	e, err := NewExtender(tmp)
	require.NoError(t, err)

	basicModules := []*node_mock.MockModule{
		{
			Name:                "ingress-nginx",
			Order:               402,
			EnabledScriptResult: true,
			// no executable bit
			Path: "./testdata/402-ingress-nginx/",
		},
		{
			Name:                "cert-manager",
			Order:               30,
			EnabledScriptResult: true,
			// no enabled script
			Path: "./testdata/030-cert-manager/",
		},
		{
			Name:                "foo-bar",
			Order:               31,
			EnabledScriptResult: true,
			Path:                "./testdata/031-foo-bar/",
		},
		{
			Name:                "node-local-dns",
			Order:               20,
			EnabledScriptResult: true,
			EnabledScriptErr:    fmt.Errorf("Exit code 1"),
			Path:                "./testdata/020-node-local-dns/",
		},
		{
			Name:                "admission-policy-engine",
			Order:               15,
			EnabledScriptResult: false,
			Path:                "./testdata/015-admission-policy-engine/",
		},
		{
			Name:                "chrony",
			Order:               45,
			EnabledScriptResult: false,
			Path:                "./testdata/045-chrony/",
		},
	}

	logLabels := map[string]string{"source": "TestExtender"}
	for _, m := range basicModules {
		e.AddBasicModule(m)
		enabled, err := e.Filter(m.Name, logLabels)
		switch m.GetName() {
		case "foo-bar":
			assert.Equal(t, true, *enabled)
			assert.Equal(t, nil, err)
		case "ingress-nginx":
			assert.Equal(t, boolNilP, enabled)
			assert.Equal(t, nil, err)
		case "cert-manager":
			assert.Equal(t, boolNilP, enabled)
			assert.Equal(t, nil, err)
		case "node-local-dns":
			assert.Equal(t, false, *enabled)
			assert.Equal(t, &exerror.PermanentError{Err: errors.New("failed to execute 'node-local-dns' module's enabled script: Exit code 1")}, err)
		case "admission-policy-engine", "chrony":
			assert.Equal(t, false, *enabled)
			assert.Equal(t, nil, err)
		}
	}

	expected := []string{"ingress-nginx", "cert-manager", "foo-bar"}
	assert.Equal(t, expected, e.enabledModules)

	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
}
