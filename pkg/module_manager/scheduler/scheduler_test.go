package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dominikbraun/graph"
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
	"github.com/flant/shell-operator/pkg/unilogger"
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

	s := NewScheduler(context.TODO(), unilogger.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	for _, m := range basicModules {
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

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

	s := NewScheduler(context.TODO(), unilogger.NewNop())

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
nodeLocalDnsEnabled: true
certManagerEnabled: true
prometheusEnabled: true
istioEnabled: true
admissionPolicyEngineEnabled: true
kubeDnsEnabled: false
`
	logLabels := map[string]string{"source": "TestGetEnabledModuleNamesByOrder"}
	basicModules := []*node_mock.MockModule{
		{
			Name:  "kube-dns",
			Order: 45,
		},
		{
			Name:  "cert-manager",
			Order: 30,
		},
		{
			Name:  "node-local-dns",
			Order: 20,
		},
		{
			Name:  "prometheus",
			Order: 20,
		},
		{
			Name:  "istio",
			Order: 20,
		},
		{
			Name:  "ingress-nginx",
			Order: 402,
		},
		{
			Name:  "admission-policy-engine",
			Order: 402,
		},
	}

	tmp, err := os.MkdirTemp(t.TempDir(), "getEnabledByOrderTest")
	require.NoError(t, err)

	s := NewScheduler(context.TODO(), unilogger.NewNop())

	valuesFile := filepath.Join(tmp, "values.yaml")
	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	se, err := static.NewExtender(tmp)
	require.NoError(t, err)

	err = s.AddExtender(se)
	require.NoError(t, err)

	for _, m := range basicModules {
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
	}

	err = s.ApplyExtenders("Static")
	require.NoError(t, err)

	_, _ = s.RecalculateGraph(logLabels)

	// get all enabled modules by order case
	enabledModules, err := s.getEnabledModuleNamesByOrder()
	assert.NoError(t, err)
	expected := map[node.NodeWeight][]string{
		node.NodeWeight(20):  {"istio", "node-local-dns", "prometheus"},
		node.NodeWeight(30):  {"cert-manager"},
		node.NodeWeight(402): {"admission-policy-engine"},
	}
	assert.Equal(t, expected, enabledModules)

	// get all enabled modules for a nonexistent order
	_, err = s.getEnabledModuleNamesByOrder(node.NodeWeight(555), node.NodeWeight(20))
	assert.Error(t, err)
	assert.Equal(t, errors.New("vertex not found"), err)

	// get all enabled modules for an existent order
	enabledModules, err = s.getEnabledModuleNamesByOrder(node.NodeWeight(20))
	assert.NoError(t, err)
	expected = map[node.NodeWeight][]string{
		node.NodeWeight(20): {"istio", "node-local-dns", "prometheus"},
	}
	assert.Equal(t, expected, enabledModules)

	// get all enabled modules for several orders
	enabledModules, err = s.getEnabledModuleNamesByOrder(node.NodeWeight(402), node.NodeWeight(30))
	assert.NoError(t, err)
	expected = map[node.NodeWeight][]string{
		node.NodeWeight(30):  {"cert-manager"},
		node.NodeWeight(402): {"admission-policy-engine"},
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

	s := NewScheduler(context.TODO(), unilogger.NewNop())

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
		err := s.AddModuleVertex(m)
		assert.NoError(t, err)
		scripte.AddBasicModule(m)
	}

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
	s := NewScheduler(context.TODO(), unilogger.NewNop())
	// no root
	assert.Equal(t, nodePtr, s.root)
	basicModuleIngress := &node_mock.MockModule{
		Name:  "ingress-nginx",
		Order: 402,
	}

	err := s.AddModuleVertex(basicModuleIngress)
	assert.NoError(t, err)

	// new module vertex is in place
	vertexIngress, err := s.dag.Vertex(basicModuleIngress.GetName())
	assert.NoError(t, err)
	assert.Equal(t, false, vertexIngress.GetState())

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

	vertexAPE, err := s.dag.Vertex(basicModuleAPE.GetName())
	assert.NoError(t, err)
	assert.Equal(t, false, vertexAPE.GetState())

	weightVertexAPE, err := s.dag.Vertex(vertexAPE.GetWeight().String())
	assert.NoError(t, err)
	assert.Equal(t, weightVertexAPE, s.root)

	_, err = s.dag.Edge(vertexAPE.GetWeight().String(), basicModuleAPE.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(vertexAPE.GetWeight().String(), vertexIngress.GetWeight().String())
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

	vertexDNS, err := s.dag.Vertex(basicModuleDNS.GetName())
	assert.NoError(t, err)

	weightVertexDNS, err := s.dag.Vertex(vertexDNS.GetWeight().String())
	assert.NoError(t, err)
	assert.Equal(t, weightVertexAPE, s.root)

	_, err = s.dag.Edge(vertexIngress.GetWeight().String(), weightVertexDNS.GetWeight().String())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexDNS.GetWeight().String(), basicModuleDNS.GetName())
	assert.NoError(t, err)

	// new vertex with the same order number
	basicModuleChrony := &node_mock.MockModule{
		Name:  "chrony",
		Order: 402,
	}

	err = s.AddModuleVertex(basicModuleChrony)
	assert.NoError(t, err)

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

	weightVertexFoo, err := s.dag.Vertex(basicModuleFoo.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexFoo.GetWeight().String(), basicModuleFoo.GetName())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexAPE.GetWeight().String(), weightVertexFoo.GetWeight().String())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexFoo.GetWeight().String(), weightVertexIngress.GetWeight().String())
	assert.NoError(t, err)

	_, err = s.dag.Edge(weightVertexAPE.GetWeight().String(), weightVertexIngress.GetWeight().String())
	assert.Error(t, err)
	assert.Equal(t, graph.ErrEdgeNotFound, err)
}

func TestSetExtendersMeta(t *testing.T) {
	s := NewScheduler(context.TODO(), unilogger.NewNop())

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
	s := NewScheduler(context.TODO(), unilogger.NewNop())
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

	summary, err := s.PrintSummary()
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

	summary, err = s.PrintSummary()
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

	summary, err = s.PrintSummary()
	assert.NoError(t, err)

	assert.Equal(t, expectedSummary, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)

	// finalize
	err = os.RemoveAll(tmp)
	require.NoError(t, err)
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
	basicModules := []*node_mock.MockModule{
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

	s := NewScheduler(context.TODO(), unilogger.NewNop())
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

	err = s.ApplyExtenders("Static")
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
	require.NoError(t, err)
	err = s.AddExtender(scripte)
	require.NoError(t, err)

	for _, v := range basicModules {
		scripte.AddBasicModule(v)
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
		"echo/KubeConfig":                             true,
		"prometheus/KubeConfig":                       true,
		"prometheus-crd/KubeConfig":                   true,
	}

	expectedDiff = nil
	expectedVerticesToUpdate = []string{}

	assert.Equal(t, expected, summary)
	assert.Equal(t, expectedDiff, diff)
	assert.Equal(t, expectedVerticesToUpdate, verticesToUpdate)
	assert.Equal(t, []string{"ScriptEnabled extender failed to filter ingress-nginx module: failed to execute 'ingress-nginx' module's enabled script: Exit code not 0"}, s.errList)

	err = os.RemoveAll(tmp)
	require.NoError(t, err)
}
