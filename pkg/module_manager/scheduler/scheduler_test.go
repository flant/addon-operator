package scheduler

import (
	"context"
	"errors"
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

func TestFilter(t *testing.T) {
	var eNil *bool
	values := `
# CE Bundle "Default"
ingressNginxEnabled: true
nodeLocalDnsEnabled: false
`
	basicModules := []*node.MockModule{
		{
			Name:  "cert-manager",
			Order: 30,
		},
		{
			Name:  "node-local-dns",
			Order: 20,
		},
		{
			Name:  "ingress-nginx",
			Order: 402,
		},
	}

	tmp, err := os.MkdirTemp(t.TempDir(), "getEnabledTest")
	require.NoError(t, err)

	s := NewScheduler(context.TODO())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	assert.NoError(t, err)

	err = s.AddExtender(se)
	assert.NoError(t, err)

	for _, m := range basicModules {
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	err = s.ApplyExtenders("Static")
	require.NoError(t, err)

	logLabels := map[string]string{"source": "TestFilter"}
	_, _ = s.RecalculateGraph(logLabels)

	enabledModules, err := s.GetEnabledModuleNames()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ingress-nginx"}, enabledModules)

	filter, err := s.Filter(static.Name, "ingress-nginx", logLabels)
	assert.NoError(t, err)
	assert.Equal(t, true, *filter)

	filter, err = s.Filter(static.Name, "cert-manager", logLabels)
	assert.NoError(t, err)
	assert.Equal(t, eNil, filter)

	filter, err = s.Filter(static.Name, "node-local-dns", logLabels)
	assert.NoError(t, err)
	assert.Equal(t, false, *filter)

	filter, err = s.Filter(dynamically_enabled.Name, "node-local-dns", logLabels)
	assert.Error(t, err)
	assert.Equal(t, eNil, filter)

	// finalize
	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
}

func TestGetEnabledModuleNames(t *testing.T) {
	values := `
# CE Bundle "Default"
ingressNginxEnabled: true
nodeLocalDnsEnabled: true
certManagerEnabled: true
`
	logLabels := map[string]string{"source": "TestGetEnabledModuleNames"}
	basicModules := []*node.MockModule{
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
			Name:                "ingress-nginx",
			Order:               402,
			EnabledScriptResult: true,
			EnabledScriptErr:    errors.New("Exit code not 0"),
			Path:                "./testdata/402-ingress-nginx/",
		},
	}

	tmp, err := os.MkdirTemp(t.TempDir(), "getEnabledTest")
	require.NoError(t, err)

	s := NewScheduler(context.TODO())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	assert.NoError(t, err)

	err = s.AddExtender(se)
	assert.NoError(t, err)

	scripte, err := script_enabled.NewExtender(tmp)
	assert.NoError(t, err)
	err = s.AddExtender(scripte)
	assert.NoError(t, err)

	for _, m := range basicModules {
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
		scripte.AddBasicModule(m)
	}

	err = s.ApplyExtenders("Static,ScriptEnabled")
	require.NoError(t, err)

	_, _ = s.RecalculateGraph(logLabels)

	enabledModules, err := s.GetEnabledModuleNames()
	assert.Error(t, err)

	assert.Equal(t, []string{}, enabledModules)

	// finalize
	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
}

func TestAddModuleVertex(t *testing.T) {
	s := NewScheduler(context.TODO())
	basicModule := &node.MockModule{
		Name:  "ingress-nginx",
		Order: 402,
	}

	err := s.AddModuleVertex(basicModule)
	assert.NoError(t, err)

	vertex, err := s.dag.Vertex(basicModule.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Vertex(vertex.GetWeight().String())
	assert.NoError(t, err)
	assert.Equal(t, false, vertex.GetState())
}

func TestRecalculateGraph(t *testing.T) {
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
	logLabels := map[string]string{"source": "TestRecalculateGraph"}
	basicModules := []*node.MockModule{
		{
			Name:                "ingress-nginx",
			Order:               402,
			EnabledScriptResult: true,
			Path:                "./testdata/402-ingress-nginx/",
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
			Path:                "./testdata/133-foo-bar/",
		},
		{
			Name:                "flant-integration",
			Order:               450,
			EnabledScriptResult: false,
			Path:                "./testdata/450-flant-integration/",
		},
		{
			Name:                "monitoring-applications",
			Order:               340,
			EnabledScriptResult: false,
			Path:                "./testdata/340-monitoring-applications/",
		},
		{
			Name:                "l2-load-balancer",
			Order:               397,
			EnabledScriptResult: true,
		},
		{
			Name:                "echo",
			Order:               909,
			EnabledScriptResult: true,
		},
		{
			Name:                "prometheus",
			Order:               340,
			EnabledScriptResult: true,
		},
		{
			Name:                "prometheus-crd",
			Order:               10,
			EnabledScriptResult: true,
		},
		{
			Name:                "openstack-cloud-provider",
			Order:               35,
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

	updated, verticesToUpdate := s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, diff, err := s.GetGraphState(logLabels)
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
		"openstack-cloud-provider/":      false,
		"prometheus-crd/":                false,
		"prometheus/":                    false,
		"echo/":                          false,
	}

	expectedDiff := map[string]bool{
		"admission-policy-engine": true,
		"cert-manager":            true,
		"chrony":                  true,
		"node-local-dns":          true,
		"flant-integration":       true,
		"monitoring-applications": true,
	}

	expectedVerticesToUpdate := []string{
		"admission-policy-engine",
		"node-local-dns",
		"cert-manager",
		"chrony",
		"foo-bar",
		"monitoring-applications",
		"l2-load-balancer",
		"flant-integration",
	}

	summary, err := s.PrintSummary()
	assert.NoError(t, err)

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	de := dynamically_enabled.NewExtender()
	go func() {
		//nolint:revive
		for range s.EventCh() {
		}
	}()

	err = s.AddExtender(de)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled")
	require.NoError(t, err)

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, diff, err = s.GetGraphState(logLabels)
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
		"prometheus-crd/":                             false,
		"prometheus/":                                 false,
		"echo/":                                       false,
	}

	expectedDiff = map[string]bool{
		"l2-load-balancer":         true,
		"openstack-cloud-provider": true,
	}

	expectedVerticesToUpdate = []string{
		"openstack-cloud-provider",
		"l2-load-balancer",
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

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

	err = s.AddExtender(kce)
	assert.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig")
	require.NoError(t, err)

	de.UpdateStatus("node-local-dns", "add", true)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, diff, err = s.GetGraphState(logLabels)
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

	expectedVerticesToUpdate = []string{
		"prometheus-crd",
		"node-local-dns",
		"cert-manager",
		"chrony",
		"foo-bar",
		"prometheus",
		"echo",
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	scripte, err := script_enabled.NewExtender(tmp)
	assert.NoError(t, err)
	err = s.AddExtender(scripte)
	assert.NoError(t, err)

	for _, v := range basicModules {
		scripte.AddBasicModule(v)
	}

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig,ScriptEnabled")
	require.NoError(t, err)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, diff, err = s.GetGraphState(logLabels)
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

	expectedVerticesToUpdate = []string{
		"foo-bar",
		"monitoring-applications",
		"flant-integration",
	}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	de.UpdateStatus("openstack-cloud-provider", "add", false)
	de.UpdateStatus("ingress-nginx", "add", true)
	de.UpdateStatus("node-local-dns", "add", true)

	updated, _ = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, false, updated)

	_, diff, err = s.GetGraphState(logLabels)
	assert.NoError(t, err)

	expected = map[string]bool{
		"admission-policy-engine/Static":              true,
		"cert-manager/KubeConfig":                     true,
		"chrony/KubeConfig":                           false,
		"node-local-dns/DynamicallyEnabled":           true,
		"foo-bar/ScriptEnabled":                       false,
		"flant-integration/ScriptEnabled":             false,
		"monitoring-applications/ScriptEnabled":       false,
		"ingress-nginx/DynamicallyEnabled":            true,
		"l2-load-balancer/DynamicallyEnabled":         true,
		"openstack-cloud-provider/DynamicallyEnabled": false,
		"echo/KubeConfig":                             true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"openstack-cloud-provider": false,
		"ingress-nginx":            true,
	}

	expectedVerticesToUpdate = []string{}

	summary, err = s.PrintSummary()
	assert.NoError(t, err)

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	basicModules[0].EnabledScriptErr = errors.New("Exit code not 0")
	scripte.AddBasicModule(basicModules[0])

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	_, diff, err = s.GetGraphState(logLabels)
	assert.Error(t, err)

	expected = map[string]bool{
		"admission-policy-engine/Static":              true,
		"cert-manager/KubeConfig":                     true,
		"chrony/KubeConfig":                           false,
		"node-local-dns/DynamicallyEnabled":           true,
		"foo-bar/ScriptEnabled":                       false,
		"flant-integration/ScriptEnabled":             false,
		"monitoring-applications/ScriptEnabled":       false,
		"ingress-nginx/DynamicallyEnabled":            true,
		"l2-load-balancer/DynamicallyEnabled":         true,
		"openstack-cloud-provider/DynamicallyEnabled": false,
		"echo/KubeConfig":                             true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = nil
	expectedVerticesToUpdate = []string{}

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)
	assert.Equal(t, []string{"Failed to execute 'ingress-nginx' module's enabled script: Exit code not 0"}, s.errList)

	s.printGraph()

	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
}
