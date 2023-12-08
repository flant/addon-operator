package modules

import (
	"encoding/json"
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

	newValues, err := st.GenerateNewConfigValues(configV, true)
	assert.NoError(t, err)
	st.SaveConfigValues(newValues)
	assert.YAMLEq(t, `
highAvailability: true
internal:
    fooBar: baz
modules:
    publicDomainTemplate: '%s.foo.bar'
xxx: yyy
`, st.GetValues(false).AsString("yaml"))
}

func TestPatchValues(t *testing.T) {
	vv := validation.NewValuesValidator()
	cb, vb, err := utils.ReadOpenAPIFiles("./testdata/global/openapi")
	require.NoError(t, err)
	err = vv.SchemaStorage.AddGlobalValuesSchemas(cb, vb)
	require.NoError(t, err)
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

	vs := NewValuesStorage("global", mcv, vv)

	vp := utils.NewValuesPatch()
	vp.Operations = append(vp.Operations, &utils.ValuesPatchOperation{
		Op:    "add",
		Path:  "/global/modules/resourcesRequests/everyNode/cpu",
		Value: json.RawMessage(`"500m"`),
	})

	vs.appendValuesPatch(*vp)

	err = vs.CommitValues()
	require.NoError(t, err)
	v := vs.GetValues(false)

	assert.YAMLEq(t, `
discovery:
    clusterControlPlaneIsHighlyAvailable: false
    d8SpecificNodeCountByRole: {}
    prometheusScrapeInterval: 30
highAvailability: false
internal:
    modules:
        kubeRBACProxyCA: {}
        resourcesRequests:
            memoryControlPlane: 0
            milliCpuControlPlane: 0
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
            cpu: 500m
            memory: 512Mi
modulesImages:
    registry: {}
    tags: {}
`, v.AsString("yaml"))
}
