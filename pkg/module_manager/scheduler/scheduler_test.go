package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/kube_config"
	extender_mock "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/mock"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/script_enabled"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	node_mock "github.com/flant/addon-operator/pkg/module_manager/scheduler/node/mock"
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
	basicModules := []*node_mock.MockModule{
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

	s := NewScheduler(context.TODO(), log.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	for _, m := range basicModules {
		err = s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	err = s.Initialize()
	require.NoError(t, err)

	err = s.ApplyExtenders("Static")
	require.NoError(t, err)

	logLabels := map[string]string{"source": "TestFilter"}
	_, _ = s.RecalculateGraph(logLabels)

	enabledModules := s.GetEnabledModuleNames()
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
	require.NoError(t, err)
}

func TestApplyExtenders(t *testing.T) {
	values := `
# CE Bundle "Default"
nodeLocalDnsEnabled: true
`
	tmp, err := os.MkdirTemp(t.TempDir(), "ApplyExtenders")
	require.NoError(t, err)

	s := NewScheduler(context.TODO(), log.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	assert.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	err = s.ApplyExtenders("A,Static")
	assert.Equal(t, errors.New("couldn't find A extender in the list of available extenders"), err)

	err = s.ApplyExtenders("A,B,Static")
	assert.Equal(t, errors.New("couldn't find A extender in the list of available extenders"), err)

	err = s.ApplyExtenders("A,B")
	assert.Equal(t, errors.New("couldn't find A extender in the list of available extenders"), err)

	err = s.ApplyExtenders("A,Static,B")
	assert.Equal(t, errors.New("couldn't find A extender in the list of available extenders"), err)

	err = s.ApplyExtenders("Static,B,A")
	assert.Equal(t, errors.New("couldn't find B extender in the list of available extenders"), err)

	err = s.ApplyExtenders("Static")
	assert.NoError(t, err)

	// finalize
	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}

func TestGetEnabledModuleNamesByOrder(t *testing.T) {
	values := `
# CE Bundle "Default"
kubeDnsEnabled: true
cniCiliumEnabled: true
nodeManagerEnabled: true
providerYandexEnabled: true
operatorPrometheusEnabled: true
prometheusCrdEnabled: true
prometheusEnabled: true
certManagerEnabled: true
nodeLocalDnsEnabled: true
ciliumHubbleEnabled: true
`
	logLabels := map[string]string{"source": "TestGetEnabledModuleNamesByOrder"}
	basicModules := []*node_mock.MockModule{
		{
			Name:     "kube-dns",
			Critical: true,
			Order:    45,
		},
		{
			Name:     "cni-cilium",
			Critical: true,
			Order:    30,
		},
		{
			Name:     "ingress-nginx",
			Critical: false,
			Order:    40,
		},
		{
			Name:     "node-manager",
			Critical: true,
			Order:    40,
		},
		{
			Name:     "provider-yandex",
			Critical: true,
			Order:    50,
		},
		{
			Name:  "prometheus-crd",
			Order: 25,
		},
		{
			Name:  "operator-prometheus",
			Order: 60,
		},
		{
			Name:  "prometheus",
			Order: 20,
		},
		{
			Name:  "cert-manager",
			Order: 50,
		},
		{
			Name:  "node-local-dns",
			Order: 230,
		},
		{
			Name:  "cilium-hubble",
			Order: 70,
		},
	}

	tmp, err := os.MkdirTemp(t.TempDir(), "getEnabledByOrderTest")
	require.NoError(t, err)

	s := NewScheduler(context.TODO(), log.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	err = s.AddExtender(&extender_mock.TopologicalOne{})
	require.NoError(t, err)

	for _, m := range basicModules {
		err = s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	err = s.Initialize()
	require.NoError(t, err)

	err = s.ApplyExtenders("Static")
	require.NoError(t, err)

	_, _ = s.RecalculateGraph(logLabels)

	// get all enabled modules by order case
	enabledModules, err := s.getModuleNamesByOrder(getEnabled, map[string]string{})
	for _, v := range enabledModules {
		slices.Sort(v)
	}
	assert.NoError(t, err)

	expected := [][]string{
		{"cni-cilium"},
		{"node-manager"},
		{"kube-dns"},
		{"provider-yandex"},
		{"cert-manager", "cilium-hubble", "node-local-dns", "prometheus-crd"},
		{"operator-prometheus"},
		{"prometheus"},
	}
	assert.Equal(t, expected, enabledModules)

	// finalize
	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}

func TestGetEnabledModuleNamesWithError(t *testing.T) {
	values := `
# CE Bundle "Default"
ingressNginxEnabled: true
nodeLocalDnsEnabled: true
certManagerEnabled: true
`
	logLabels := map[string]string{"source": "TestGetEnabledModuleNames"}
	basicModules := []*node_mock.MockModule{
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

	s := NewScheduler(context.TODO(), log.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	assert.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	scripte, err := script_enabled.NewExtender(tmp)
	require.NoError(t, err)
	err = s.AddExtender(scripte)
	require.NoError(t, err)

	for _, m := range basicModules {
		err = s.AddModuleVertex(m)
		assert.NoError(t, err)
		scripte.AddBasicModule(m)
	}

	err = s.Initialize()
	require.NoError(t, err)

	err = s.ApplyExtenders("Static,ScriptEnabled")
	require.NoError(t, err)

	_, _ = s.RecalculateGraph(logLabels)

	enabledModules := s.GetEnabledModuleNames()
	assert.Equal(t, []string{}, enabledModules)

	// finalize
	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}

func TestAddModuleVertex(t *testing.T) {
	var nodePtr *node.Node
	s := NewScheduler(context.TODO(), log.NewNop())

	err := s.AddExtender(&extender_mock.TopologicalOne{})
	require.NoError(t, err)

	// no root
	assert.Equal(t, nodePtr, s.root)
	basicModuleIngress := &node_mock.MockModule{
		Name:  "ingress-nginx",
		Order: 402,
	}

	err = s.AddModuleVertex(basicModuleIngress)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	// new module vertex is in place
	vertexIngress, err := s.dag.Vertex(basicModuleIngress.GetName())
	assert.NoError(t, err)
	assert.Equal(t, false, vertexIngress.IsEnabled())

	weightVertexIngress, err := s.dag.Vertex(vertexIngress.GetWeight().String())
	assert.NoError(t, err)

	assert.Equal(t, weightVertexIngress, s.root)

	_, err = s.dag.Edge(vertexIngress.GetWeight().String(), basicModuleIngress.GetName())
	assert.NoError(t, err)

	// new vertex with a lower order number - new root
	basicModuleAPE := &node_mock.MockModule{
		Name:  "admission-policy-engine",
		Order: 42,
	}

	err = s.AddModuleVertex(basicModuleAPE)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	vertexAPE, err := s.dag.Vertex(basicModuleAPE.GetName())
	assert.NoError(t, err)
	assert.Equal(t, false, vertexAPE.IsEnabled())

	weightVertexAPE, err := s.dag.Vertex(vertexAPE.GetWeight().String())
	assert.NoError(t, err)
	assert.Equal(t, weightVertexAPE, s.root)

	_, err = s.dag.Edge(vertexAPE.GetWeight().String(), basicModuleAPE.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(vertexIngress.GetWeight().String(), basicModuleIngress.GetName())
	assert.NoError(t, err)

	// new vertex with a higher order number
	basicModuleDNS := &node_mock.MockModule{
		Name:  "node-local-dns",
		Order: 510,
	}

	err = s.AddModuleVertex(basicModuleDNS)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	vertexDNS, err := s.dag.Vertex(basicModuleDNS.GetName())
	assert.NoError(t, err)

	weightVertexDNS, err := s.dag.Vertex(vertexDNS.GetWeight().String())
	assert.NoError(t, err)
	assert.Equal(t, weightVertexAPE, s.root)

	_, err = s.dag.Edge(weightVertexDNS.GetWeight().String(), basicModuleDNS.GetName())
	assert.NoError(t, err)

	// new vertex with the same order number
	basicModuleChrony := &node_mock.MockModule{
		Name:  "chrony",
		Order: 402,
	}

	err = s.AddModuleVertex(basicModuleChrony)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	_, err = s.dag.Vertex(basicModuleChrony.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(vertexIngress.GetWeight().String(), basicModuleChrony.GetName())
	assert.NoError(t, err)

	// new vertex between two existing vertices

	basicModuleFoo := &node_mock.MockModule{
		Name:  "foo",
		Order: 300,
	}

	err = s.AddModuleVertex(basicModuleFoo)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	vertexFoo, err := s.dag.Vertex(basicModuleFoo.GetName())
	assert.NoError(t, err)

	weightVertexFoo, err := s.dag.Vertex(vertexFoo.GetWeight().String())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexFoo.GetName(), basicModuleFoo.GetName())
	assert.NoError(t, err)

	// new vertex with topological hints, dependent on "foo" and "bar" vertices
	basicModuleFooBar := &node_mock.MockModule{
		Name:  "foobar",
		Order: 400,
	}

	err = s.AddModuleVertex(basicModuleFooBar)
	assert.NoError(t, err)

	err = s.Initialize()
	require.NoError(t, err)

	vertexFooBar, err := s.dag.Vertex(basicModuleFooBar.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(vertexFoo.GetName(), vertexFooBar.GetName())
	assert.NoError(t, err)
}

func TestSetExtendersMeta(t *testing.T) {
	s := NewScheduler(context.TODO(), log.NewNop())

	err := s.AddExtender(&extender_mock.FilterOne{})
	require.NoError(t, err)
	err = s.AddExtender(&extender_mock.FilterTwo{})
	require.NoError(t, err)
	err = s.AddExtender(&extender_mock.FilterThree{})
	require.NoError(t, err)
	err = s.AddExtender(&extender_mock.TerminatorOne{})
	require.NoError(t, err)
	err = s.AddExtender(&extender_mock.TerminatorTwo{})
	require.NoError(t, err)
	err = s.ApplyExtenders("FilterOne,FilterTwo,FilterThree,TerminatorOne,TerminatorTwo")
	require.NoError(t, err)

	expected := []bool{
		true,
		true,
		false,
		false,
		false,
	}

	for i, e := range s.extenders {
		if expected[i] != e.filterAhead {
			t.Errorf("extender's %s filterAhead value %v not equal to expected %v", e.ext.Name(), e.filterAhead, expected[i])
		}
	}

	err = s.ApplyExtenders("TerminatorOne,TerminatorTwo,FilterOne,FilterTwo,FilterThree")
	assert.NoError(t, err)

	expected = []bool{
		true,
		true,
		true,
		true,
		false,
	}

	for i, e := range s.extenders {
		if expected[i] != e.filterAhead {
			t.Errorf("extender's %s filterAhead value %v not equal to expected %v", e.ext.Name(), e.filterAhead, expected[i])
		}
	}

	err = s.AddExtender(&extender_mock.TerminatorThree{})
	assert.NoError(t, err)
	err = s.ApplyExtenders("TerminatorOne,FilterOne,TerminatorTwo,FilterTwo,FilterThree,TerminatorThree")
	assert.NoError(t, err)

	expected = []bool{
		true,
		true,
		true,
		true,
		false,
		false,
	}

	for i, e := range s.extenders {
		if expected[i] != e.filterAhead {
			t.Errorf("extender's %s filterAhead value %v not equal to expected %v", e.ext.Name(), e.filterAhead, expected[i])
		}
	}
}

func TestExtendersOrder(t *testing.T) {
	values := `
admissionPolicyEngineEnabled: true
kubeDnsEnabled: false
`
	logLabels := map[string]string{"source": "TestExtendersOrder"}
	basicModules := []*node_mock.MockModule{
		{
			Name:                "admission-policy-engine",
			Order:               15,
			EnabledScriptResult: true,
			Path:                "./testdata/015-admission-policy-engine/",
		},
		{
			Name:                "kube-dns",
			Order:               42,
			EnabledScriptResult: false,
		},
		{
			Name:                "ingress-nginx",
			Order:               420,
			EnabledScriptResult: true,
		},
	}
	s := NewScheduler(context.TODO(), log.NewNop())
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
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	require.NoError(t, err)

	scripte, err := script_enabled.NewExtender(tmp)
	require.NoError(t, err)
	err = s.AddExtender(scripte)
	require.NoError(t, err)

	for _, v := range basicModules {
		scripte.AddBasicModule(v)
	}

	err = s.Initialize()
	require.NoError(t, err)

	// terminator goes last
	err = s.ApplyExtenders("Static,ScriptEnabled")
	require.NoError(t, err)

	updated, verticesToUpdate := s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	_, _, diff, err := s.GetGraphState(logLabels)
	assert.NoError(t, err)

	expectedSummary := map[string]bool{
		"admission-policy-engine/Static": true,
		"kube-dns/Static":                false,
		"ingress-nginx/":                 false,
	}

	expectedDiff := map[string]bool{
		"admission-policy-engine": true,
	}

	expectedVerticesToUpdate := []string{
		"admission-policy-engine",
		"kube-dns",
	}

	summary, err := s.printSummary()
	assert.NoError(t, err)

	assert.Equal(t, expectedSummary, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// revert extenders order
	err = s.ApplyExtenders("ScriptEnabled,Static")
	require.NoError(t, err)

	expectedSummary = map[string]bool{
		"admission-policy-engine/Static": true,
		"kube-dns/Static":                false,
		"ingress-nginx/":                 false,
	}

	expectedDiff = map[string]bool{}

	expectedVerticesToUpdate = []string{}

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, false, updated)

	_, _, diff, err = s.GetGraphState(logLabels)
	assert.NoError(t, err)

	summary, err = s.printSummary()
	assert.NoError(t, err)

	assert.Equal(t, expectedSummary, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// update script_enabled extender so that the module is disabled
	basicModules[0].EnabledScriptResult = false

	for _, v := range basicModules {
		scripte.AddBasicModule(v)
	}

	expectedSummary = map[string]bool{
		"admission-policy-engine/ScriptEnabled": false,
		"kube-dns/Static":                       false,
		"ingress-nginx/":                        false,
	}

	expectedDiff = map[string]bool{
		"admission-policy-engine": false,
	}

	expectedVerticesToUpdate = []string{
		"admission-policy-engine",
	}

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	_, _, diff, err = s.GetGraphState(logLabels)
	assert.NoError(t, err)

	summary, err = s.printSummary()
	assert.NoError(t, err)

	assert.Equal(t, expectedSummary, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// finalize
	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}

func TestRecalculateGraph(t *testing.T) {
	// initial values
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

myModuleEnabled: true

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
	enabledScriptInternalListOfEnabledModules := make([]string, 0)
	basicModules := []*node_mock.MockModule{
		{
			Name:                "ingress-nginx",
			Order:               402,
			EnabledScriptResult: true,
			Path:                "./testdata/402-ingress-nginx/",
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "cert-manager",
			Order:               20,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
			Path:                "./testdata/20-cert-manager",
		},
		{
			Name:                "node-local-dns",
			Order:               20,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "admission-policy-engine",
			Order:               15,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "chrony",
			Order:               45,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "foo-bar",
			Order:               133,
			EnabledScriptResult: false,
			Path:                "./testdata/133-foo-bar/",
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "flant-integration",
			Order:               450,
			EnabledScriptResult: false,
			Path:                "./testdata/450-flant-integration/",
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "monitoring-applications",
			Order:               340,
			EnabledScriptResult: false,
			Path:                "./testdata/340-monitoring-applications/",
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "l2-load-balancer",
			Order:               397,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                  "test-echo",
			Order:                 909,
			EnabledScriptResult:   true,
			ListOfRequiredModules: []string{"cert-manager", "prometheus"},
			EnabledModules:        &enabledScriptInternalListOfEnabledModules,
			Path:                  "./testdata/909-test-echo",
		},
		{
			Name:                "prometheus",
			Order:               340,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
			Path:                "./testdata/340-prometheus/",
		},
		{
			Name:                "prometheus-crd",
			Order:               10,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "operator-prometheus",
			Order:               15,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
		{
			Name:                "openstack-cloud-provider",
			Order:               30,
			EnabledScriptResult: true,
			EnabledModules:      &enabledScriptInternalListOfEnabledModules,
		},
	}

	// init scheduler
	s := NewScheduler(context.TODO(), log.NewNop())

	// add and apply topological extenders
	err := s.AddExtender(&extender_mock.TopologicalOne{})
	require.NoError(t, err)

	for _, m := range basicModules {
		err = s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	err = s.Initialize()
	require.NoError(t, err)

	tmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)
	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	// add and apply static extender
	se, err := static.NewExtender(tmp)
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	err = s.ApplyExtenders("TopologicalOne,Static")
	require.NoError(t, err)

	updated, verticesToUpdate := s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, _, diff, err := s.GetGraphState(logLabels)
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
		"operator-prometheus/":           false,
		"prometheus-crd/":                false,
		"prometheus/":                    false,
		"test-echo/":                     false,
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
		"cert-manager",
		"chrony",
		"flant-integration",
		"foo-bar",
		"l2-load-balancer",
		"monitoring-applications",
		"node-local-dns",
	}

	summary, err := s.printSummary()
	assert.NoError(t, err)

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// add and apply dynamic extender
	de := dynamically_enabled.NewExtender()
	go func() {
		//nolint:revive
		for range s.EventCh() {
		}
	}()

	err = s.AddExtender(de)
	require.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled")
	require.NoError(t, err)

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, _, diff, err = s.GetGraphState(logLabels)
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
		"operator-prometheus/":                        false,
		"prometheus-crd/":                             false,
		"prometheus/":                                 false,
		"test-echo/":                                  false,
	}

	expectedDiff = map[string]bool{
		"l2-load-balancer":         true,
		"openstack-cloud-provider": true,
	}

	expectedVerticesToUpdate = []string{
		"l2-load-balancer",
		"openstack-cloud-provider",
	}

	summary, err = s.printSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// add amd apply kube config extender
	kce := kube_config.NewExtender(kcmMock{
		modulesStatus: map[string]bool{
			"cert-manager":        true,
			"chrony":              false,
			"foo-bar":             true,
			"test-echo":           true,
			"prometheus":          true,
			"prometheus-crd":      true,
			"operator-prometheus": true,
		},
	})

	err = s.AddExtender(kce)
	require.NoError(t, err)

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig")
	require.NoError(t, err)

	de.UpdateStatus("node-local-dns", "add", true)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, _, diff, err = s.GetGraphState(logLabels)
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
		"operator-prometheus/KubeConfig":              true,
		"test-echo/KubeConfig":                        true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"test-echo":           true,
		"prometheus":          true,
		"prometheus-crd":      true,
		"foo-bar":             true,
		"operator-prometheus": true,
		"chrony":              false,
	}

	expectedVerticesToUpdate = []string{
		"cert-manager",
		"chrony",
		"foo-bar",
		"node-local-dns",
		"operator-prometheus",
		"prometheus",
		"prometheus-crd",
		"test-echo",
	}

	summary, err = s.printSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// add and apply script_enabled extender
	scriptext, err := script_enabled.NewExtender(tmp)
	require.NoError(t, err)
	err = s.AddExtender(scriptext)
	require.NoError(t, err)

	for _, v := range basicModules {
		scriptext.AddBasicModule(v)
	}

	err = s.ApplyExtenders("Static,DynamicallyEnabled,KubeConfig,ScriptEnabled")
	require.NoError(t, err)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)
	_, _, diff, err = s.GetGraphState(logLabels)
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
		"operator-prometheus/KubeConfig":              true,
		"test-echo/KubeConfig":                        true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"monitoring-applications": false,
		"foo-bar":                 false,
		"flant-integration":       false,
	}

	expectedVerticesToUpdate = []string{
		"flant-integration",
		"foo-bar",
		"monitoring-applications",
	}

	summary, err = s.printSummary()
	assert.NoError(t, err)
	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// file, _ := os.Create("/tmp/mygraph.gv")
	// _ = draw.DOT(s.dag, file)

	// some tests with dynamic extender
	de.UpdateStatus("openstack-cloud-provider", "add", false)
	de.UpdateStatus("ingress-nginx", "add", true)
	de.UpdateStatus("node-local-dns", "add", true)

	enabledScriptInternalListOfEnabledModules = make([]string, 0)

	updated, _ = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	enabledScriptInternalListOfEnabledModules = make([]string, 0)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, false, updated)

	_, _, diff, err = s.GetGraphState(logLabels)
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
		"operator-prometheus/KubeConfig":              true,
		"test-echo/KubeConfig":                        true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = map[string]bool{
		"openstack-cloud-provider": false,
		"ingress-nginx":            true,
	}

	expectedVerticesToUpdate = []string{}

	summary, err = s.printSummary()
	assert.NoError(t, err)

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	basicModules[0].EnabledScriptErr = errors.New("exit code not 0")
	scriptext.AddBasicModule(basicModules[0])

	enabledScriptInternalListOfEnabledModules = make([]string, 0)

	updated, verticesToUpdate = s.RecalculateGraph(logLabels)
	assert.Equal(t, true, updated)

	_, _, diff, err = s.GetGraphState(logLabels)
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
		"operator-prometheus/KubeConfig":              true,
		"test-echo/KubeConfig":                        true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = nil
	expectedVerticesToUpdate = nil

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)
	assert.Equal(t, "1 error occurred:\n\t* ScriptEnabled extender failed to filter ingress-nginx module: failed to execute 'ingress-nginx' module's enabled script: exit code not 0\n\n", s.err.Error())

	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}
