package modules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/values/validation"

	"github.com/flant/addon-operator/pkg/utils"
)

func TestMerge(t *testing.T) {
	cfg := `
type: object
default: {}
additionalProperties: false
properties:
  foo:
    type: string
    default: "be"
  echo:
    type: string
  abcd:
    type: string
    default: "exists"
`
	vv := validation.NewValuesValidator()
	err := vv.SchemaStorage.AddModuleValuesSchemas("qqqq", []byte(cfg), nil)
	require.NoError(t, err)
	initial := utils.Values{}
	static := utils.Values{"foo": "bar", "echo": "zzz"}
	tra := &applyDefaultsForModule{
		ModuleName:      "qqqq",
		SchemaType:      validation.ConfigValuesSchema,
		ValuesValidator: vv,
	}
	config := utils.Values{"foo": "baz"}
	result := mergeLayers(initial, static, tra, config)

	fmt.Println("1", result)
	fmt.Println("2", initial)
	fmt.Println("3", static)
	fmt.Println("4", config)
}
