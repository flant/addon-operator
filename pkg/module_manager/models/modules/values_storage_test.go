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
