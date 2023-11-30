package modules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

func TestHandleModulePatch(t *testing.T) {
	valuesStr := `
foo: 
  bar: baz
`
	value, err := utils.NewValuesFromBytes([]byte(valuesStr))
	require.NoError(t, err)
	vv := validation.NewValuesValidator()
	bm := NewBasicModule("test-1", "/tmp/test", 100, value, vv)

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
	res, err := bm.handleModuleValuesPatch(bm.GetValues(true), patch)
	require.NoError(t, err)
	assert.True(t, res.ValuesChanged)
	assert.YAMLEq(t, `
foo: 
  xxx: yyy
`,
		res.Values.AsString("yaml"))
}
