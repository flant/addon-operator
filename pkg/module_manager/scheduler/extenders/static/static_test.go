package static

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtender(t *testing.T) {
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

	additionalValues := `
l2LoadBalancerEnabled: true
ingressNginxEnabled: true
`

	tmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)
	valuesFile := filepath.Join(tmp, "values.yaml")

	err = os.WriteFile(valuesFile, []byte(values), 0o644)
	require.NoError(t, err)

	additionalTmp, err := os.MkdirTemp(t.TempDir(), "values-test")
	require.NoError(t, err)
	additionalValuesFile := filepath.Join(additionalTmp, "values.yaml")

	err = os.WriteFile(additionalValuesFile, []byte(additionalValues), 0o644)
	require.NoError(t, err)

	e, err := NewExtender(fmt.Sprintf("%s:%s", tmp, additionalTmp))
	assert.NoError(t, err)

	expected := map[string]bool{
		"admission-policy-engine": true,
		"cert-manager":            true,
		"chrony":                  true,
		"node-local-dns":          true,
		"foo-bar":                 false,
		"flant-integration":       true,
		"monitoring-applications": true,
		"l2-load-balancer":        true,
		"ingress-nginx":           true,
	}

	assert.Equal(t, expected, e.modulesStatus)
	err = os.RemoveAll(tmp)
	assert.NoError(t, err)
	err = os.RemoveAll(additionalTmp)
	assert.NoError(t, err)
}
