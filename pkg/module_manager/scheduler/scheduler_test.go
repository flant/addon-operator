package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/kube_config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/script_enabled"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

type kcmMock struct {
	modulesStatus map[string]bool
}

func (k kcmMock) IsModuleEnabled(moduleName string) *bool {
	if v, ok := k.modulesStatus[moduleName]; ok {
		return &v
	}
	return nil
}

func (k kcmMock) KubeConfigEventCh() chan config.KubeConfigEvent {
	return make(chan config.KubeConfigEvent)
}

func TestUpdateAndApplyNewState(t *testing.T) {
	values := `
# Default global values section
# todo remove duplicate config values they should be in global-hooks/openapi/config-values.yaml only
# now we have strange behaviour in template tests
# probably, test helm render does not get defaults from global-hooks/openapi/config-values.yaml
global:
  modules:
    ingressClass: nginx
    placement: {}
    https:
      mode: CertManager
      certManager:
        clusterIssuerName: letsencrypt
    resourcesRequests:
      everyNode:
        cpu: 300m
        memory: 512Mi
# CE Bundle "Default"
admissionPolicyEngineEnabled: true
certManagerEnabled: true
chronyEnabled: true

# BE Bundle "Default"
nodeLocalDnsEnabled: true

# EE Bundle "Default"
fooBarEnabled: false

# FE Bundle "Default"
flantIntegrationEnabled: true
monitoringApplicationsEnabled: true
l2LoadBalancerEnabled: false
`
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
			EnabledScriptResult: true,
		},
		{
			Name:                "chrony",
			Order:               45,
			EnabledScriptResult: true,
		},
		{
			Name:                "foo-bar",
			Order:               133,
			EnabledScriptResult: false,
		},
		{
			Name:                "flant-integration",
			Order:               450,
			EnabledScriptResult: false,
		},
		{
			Name:                "monitoring-applications",
			Order:               340,
			EnabledScriptResult: false,
		},
		{
			Name:                "l2-load-balancer",
			Order:               397,
			EnabledScriptResult: true,
		},
	}

	s := NewScheduler(context.TODO())
	for _, m := range basicModules {
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	tmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)
	valuesFile := filepath.Join(tmp, "values.yaml")

	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	assert.NoError(t, err)

	err = s.AddExtender(se)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static")
	require.NoError(t, err)

	diff, err := s.UpdateAndApplyNewState()
	assert.NoError(t, err)

	expected := map[string]bool{
		"admission-policy-engine/Static": true,
		"cert-manager/Static":            true,
		"chrony/Static":                  true,
		"node-local-dns/Static":          true,
		"foo-bar/Static":                 false,
		"flant-integration/Static":       true,
		"monitoring-applications/Static": true,
		"ingress-nginx/":                 false,
		"l2-load-balancer/Static":        false,
	}

	expectedDiff := map[string]bool{
		"admission-policy-engine": true,
		"cert-manager":            true,
		"chrony":                  true,
		"node-local-dns":          true,
		"flant-integration":       true,
		"monitoring-applications": true,
	}

	summary, err := s.PrintSummary()
	assert.NoError(t, err)

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)

	de := dynamically_enabled.NewExtender()

	err = s.AddModuleVertex(node.ModuleMock{
		Name:                "openstack-cloud-provider",
		Order:               35,
		EnabledScriptResult: true,
	})
	assert.NoError(t, err)

	err = s.AddExtender(de)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled")
	require.NoError(t, err)

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	diff, err = s.UpdateAndApplyNewState()
	assert.NoError(t, err)

	expected = map[string]bool{
		"admission-policy-engine/Static":              true,
		"cert-manager/Static":                         true,
		"chrony/Static":                               true,
		"node-local-dns/Static":                       true,
		"foo-bar/Static":                              false,
		"flant-integration/Static":                    true,
		"monitoring-applications/Static":              true,
		"ingress-nginx/":                              false,
		"l2-load-balancer/DynamicallyEnabled":         true,
		"openstack-cloud-provider/DynamicallyEnabled": true,
	}

	expectedDiff = map[string]bool{
		"l2-load-balancer":         true,
		"openstack-cloud-provider": true,
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)

	kce := kube_config.NewExtender(kcmMock{
		modulesStatus: map[string]bool{
			"cert-manager":   true,
			"chrony":         false,
			"foo-bar":        true,
			"echo":           true,
			"prometheus":     true,
			"prometheus-crd": true,
		},
	})

	err = s.AddModuleVertex(node.ModuleMock{
		Name:                "echo",
		Order:               909,
		EnabledScriptResult: true,
	})
	assert.NoError(t, err)

	err = s.AddModuleVertex(node.ModuleMock{
		Name:                "prometheus",
		Order:               340,
		EnabledScriptResult: true,
	})
	assert.NoError(t, err)

	err = s.AddModuleVertex(node.ModuleMock{
		Name:                "prometheus-crd",
		Order:               10,
		EnabledScriptResult: true,
	})
	assert.NoError(t, err)

	err = s.AddExtender(kce)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig")
	require.NoError(t, err)

	de.UpdateStatus("node-local-dns", "add", true)

	diff, err = s.UpdateAndApplyNewState()
	assert.NoError(t, err)

	expected = map[string]bool{
		"admission-policy-engine/Static":              true,
		"cert-manager/KubeConfig":                     true,
		"chrony/KubeConfig":                           false,
		"node-local-dns/DynamicallyEnabled":           true,
		"foo-bar/KubeConfig":                          true,
		"flant-integration/Static":                    true,
		"monitoring-applications/Static":              true,
		"ingress-nginx/":                              false,
		"l2-load-balancer/DynamicallyEnabled":         true,
		"openstack-cloud-provider/DynamicallyEnabled": true,
		"echo/KubeConfig":                             true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"echo":           true,
		"prometheus":     true,
		"prometheus-crd": true,
		"foo-bar":        true,
		"chrony":         false,
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)

	scripte, err := script_enabled.NewExtender(tmp)
	assert.NoError(t, err)
	err = s.AddExtender(scripte)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig,ScriptEnabled")
	require.NoError(t, err)

	diff, err = s.UpdateAndApplyNewState()
	assert.NoError(t, err)

	expected = map[string]bool{
		"admission-policy-engine/Static":              true,
		"cert-manager/KubeConfig":                     true,
		"chrony/KubeConfig":                           false,
		"node-local-dns/DynamicallyEnabled":           true,
		"foo-bar/ScriptEnabled":                       false,
		"flant-integration/ScriptEnabled":             false,
		"monitoring-applications/ScriptEnabled":       false,
		"ingress-nginx/":                              false,
		"l2-load-balancer/DynamicallyEnabled":         true,
		"openstack-cloud-provider/DynamicallyEnabled": true,
		"echo/KubeConfig":                             true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"monitoring-applications": false,
		"foo-bar":                 false,
		"flant-integration":       false,
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)

	stateChanged, err := s.StateChanged(de.Name(), "baremetall")
	assert.Error(t, err)
	assert.Equal(t, stateChanged, false)

	de.UpdateStatus("openstack-cloud-provider", "add", false)
	stateChanged, err = s.StateChanged(de.Name(), "openstack-cloud-provider")
	assert.NoError(t, err)
	assert.Equal(t, stateChanged, true)

	de.UpdateStatus("node-local-dns", "add", true)
	stateChanged, err = s.StateChanged(de.Name(), "node-local-dns")
	assert.NoError(t, err)
	assert.Equal(t, stateChanged, false)

	_, err = s.UpdateAndApplyNewState()
	assert.NoError(t, err)

	s.printGraph()

	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
}
