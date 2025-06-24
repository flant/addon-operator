package modules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkutils "github.com/deckhouse/module-sdk/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/utils"
)

func TestHandleModulePatch(t *testing.T) {
	valuesStr := `
foo: 
  bar: baz
`
	value, err := utils.NewValuesFromBytes([]byte(valuesStr))
	require.NoError(t, err)
	bm, err := NewBasicModule("test-1", "/tmp/test", 100, value, nil, nil)
	require.NoError(t, err)

	patch := utils.ValuesPatch{Operations: []*sdkutils.ValuesPatchOperation{
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

func getHookName(hookPath string) string {
	hooksIdx := strings.Index(hookPath, "/hooks/")
	if hooksIdx == -1 {
		return filepath.Base(hookPath)
	}
	relPath := hookPath[hooksIdx+len("/hooks/"):]
	return filepath.Join("hooks", relPath)
}
func TestHookNameFormat(t *testing.T) {
	//module := &BasicModule{Path: "/deckhouse/modules/echo"}
	hookPath := "/deckhouse/modules/echo/v1.2.3/dddd/test.sh"
	expected := "hooks/test.sh"

	actual := getHookName(hookPath)
	if actual != expected {
		t.Errorf("Expected %q, got %q", expected, actual)
	}

}

func TestIsFileBatchHook(t *testing.T) {
	hookPath := "./testdata/batchhook"

	err := os.WriteFile(hookPath, []byte(`#!/bin/bash
if [ "${1}" == "hook" ] && [ "${2}" == "list" ]; then
	echo "Found 3 items"
fi
`), 0o555)
	require.NoError(t, err)

	defer os.Remove(hookPath)

	fileInfo, err := os.Stat(hookPath)
	require.NoError(t, err)

	err = IsFileBatchHook("moduleName", hookPath, fileInfo)
	require.NoError(t, err)
}
