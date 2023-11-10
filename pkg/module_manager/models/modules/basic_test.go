package modules

import (
	"encoding/json"
	"testing"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleModulePatch(t *testing.T) {
	valuesStr := `
foo: 
  bar: baz
`
	value, err := utils.NewValuesFromBytes([]byte(valuesStr))
	require.NoError(t, err)
	vv := validation.NewValuesValidator()
	vs := NewValuesStorage(value, vv)
	bm := NewBasicModule("test-1", "/tmp/test", 100, true, vs, vv)

	patch := utils.ValuesPatch{Operations: []*utils.ValuesPatchOperation{
		{
			Op:    "add",
			Path:  "/test1/foo/xxx",
			Value: json.RawMessage(`"yyy"`),
		},
		{
			Op:    "remove",
			Path:  "/test1/foo/bar",
			Value: json.RawMessage(`"zxc"`),
		},
	}}
	res, err := bm.handleModuleValuesPatch(bm.GetValues(), patch)
	assert.True(t, res.ValuesChanged)
	assert.YAMLEq(t, `
foo: 
  xxx: yyy
`,
		res.Values.AsString("yaml"))
}
