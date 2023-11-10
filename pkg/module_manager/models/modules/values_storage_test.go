package modules

import (
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
`

	initial := utils.Values{}
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

	err = st.PreCommitConfigValues("global", configV)
	assert.NoError(t, err)
}
