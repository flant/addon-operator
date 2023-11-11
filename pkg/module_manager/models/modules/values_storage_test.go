package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

func TestSetConfigValues(t *testing.T) {
	cfg := `
type: object
default: {}
additionalProperties: false
properties:
  xxx:
    type: string
  highAvailability:
    type: boolean
  modules:
    additionalProperties: false
    default: {}
    type: object
    properties:
      publicDomainTemplate:
        type: string
        pattern: '^(%s([-a-z0-9]*[a-z0-9])?|[a-z0-9]([-a-z0-9]*)?%s([-a-z0-9]*)?[a-z0-9]|[a-z0-9]([-a-z0-9]*)?%s)(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$'
`

	vcfg := `
x-extend:
  schema: config-values.yaml
type: object
default: {}
properties:
  internal:
    type: object
    default: {}
    properties:
      fooBar:
        type: string
        default: baz
`

	initial := utils.Values{
		"xxx": "yyy",
	}
	vv := validation.NewValuesValidator()
	err := vv.SchemaStorage.AddGlobalValuesSchemas([]byte(cfg), []byte(vcfg))
	require.NoError(t, err)
	st := NewValuesStorage("global", initial, vv)

	configV := utils.Values{
		"highAvailability": true,
		"modules": map[string]interface{}{
			"publicDomainTemplate": "%s.foo.bar",
		},
	}

	err = st.PreCommitConfigValues(configV)
	assert.NoError(t, err)
	st.CommitConfigValues()
	st.calculateResultValues()
	fmt.Println(st.GetValues())
}

func TestPreCommitValues(t *testing.T) {
	vv := validation.NewValuesValidator()
	cb, vb, err := utils.ReadOpenAPIFiles("./testdata/global/openapi")
	require.NoError(t, err)
	err = vv.SchemaStorage.AddGlobalValuesSchemas(cb, vb)
	require.NoError(t, err)

	vs := NewValuesStorage("global", utils.Values{}, vv)

	mcv, err := utils.NewValuesFromBytes([]byte(`
highAvailability: false
modules:
  https:
    certManager:
      clusterIssuerName: letsencrypt
    mode: CertManager
  ingressClass: nginx
  placement: {}
  publicDomainTemplate: '%s.example.com'
  resourcesRequests:
    controlPlane: {}
    everyNode:
      cpu: 300m
      memory: 512Mi
`))
	require.NoError(t, err)

	patchValues, err := utils.NewValuesFromBytes([]byte(`
clusterIsBootstrapped: true
deckhouseEdition: FE
deckhouseVersion: dev
discovery:
  clusterControlPlaneIsHighlyAvailable: false
  d8SpecificNodeCountByRole: {}
  kubernetesCA: XXX
  prometheusScrapeInterval: 30
highAvailability: false
internal:
  modules:
    kubeRBACProxyCA: {}
    resourcesRequests:
      memoryControlPlane: 3086170981
      milliCpuControlPlane: 1480
modules:
  https:
    certManager:
      clusterIssuerName: letsencrypt
    mode: CertManager
  ingressClass: nginx
  placement: {}
  publicDomainTemplate: '%s.exmaple.com'
  resourcesRequests:
    controlPlane: {}
    everyNode:
      cpu: 300m
      memory: 512Mi
`))
	require.NoError(t, err)

	vs.mergedConfigValues = mcv

	err = vs.PreCommitValues(patchValues)
	require.NoError(t, err)
}
