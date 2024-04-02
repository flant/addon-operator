package script_enabled

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

func TestExtender(t *testing.T) {
	tmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)

	e, err := NewExtender(tmp)
	require.NoError(t, err)

	basicModules := []node.ModuleMock{
		{
			Name:                "ingress-nginx",
			Order:               402,
			EnabledScriptResult: true,
		},
		{
			Name:                "cert-manager",
			Order:               30,
			EnabledScriptResult: true,
		},
		{
			Name:                "node-local-dns",
			Order:               20,
			EnabledScriptResult: true,
		},
		{
			Name:                "admission-policy-engine",
			Order:               15,
			EnabledScriptResult: false,
		},
		{
			Name:                "chrony",
			Order:               45,
			EnabledScriptResult: false,
		},
	}

	for _, m := range basicModules {
		enabled, err := e.Filter(m)
		assert.NoError(t, err)
		switch m.GetName() {
		case "ingress-nginx", "cert-manager", "node-local-dns":
			assert.Equal(t, *enabled, true)
		case "admission-policy-engine", "chrony":
			assert.Equal(t, *enabled, false)
		}
	}
	expected := []string{"ingress-nginx", "cert-manager", "node-local-dns"}
	assert.Equal(t, e.enabledModules, expected)
}
