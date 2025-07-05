package modules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	sdkutils "github.com/deckhouse/module-sdk/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	metric_storage "github.com/flant/shell-operator/pkg/metric_storage"
)

func TestHandleModulePatch(t *testing.T) {
	valuesStr := `
foo: 
  bar: baz
`
	value, err := utils.NewValuesFromBytes([]byte(valuesStr))
	require.NoError(t, err)
	bm, err := NewBasicModule(&Config{Name: "test-1", Path: "/tmp/test", Weight: 100}, value, nil, nil)
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

func TestConvertHookName(t *testing.T) {
	name, err := normalizeHookPath("/modules/console/v1.35.3", "/modules/console/v1.35.3/hooks/https/copy_cert.py")
	require.NoError(t, err)
	require.Equal(t, "hooks/https/copy_cert.py", name)
}

func TestHookNameNormalizedInStorage(t *testing.T) {
	modulePath := "/tmp/modules/console/v1.2.3"
	err := os.MkdirAll(filepath.Join(modulePath, "hooks/https"), 0o755)
	require.NoError(t, err)

	hookPath := filepath.Join(modulePath, "hooks/https/test_hook.py")
	err = os.WriteFile(hookPath, []byte(`#!/usr/bin/env python3
import sys
import yaml

if "--config" in sys.argv:
    print(yaml.dump({
        "configVersion": "v1",
        "onStartup": 10
    }))
    sys.exit(0)
sys.exit(0)
`), 0o755)
	require.NoError(t, err)

	defer os.RemoveAll("/tmp/modules")

	valuesStr := `
foo: 
  bar: baz
`
	value, err := utils.NewValuesFromBytes([]byte(valuesStr))
	require.NoError(t, err)

	bm, err := NewBasicModule("console", modulePath, 10, value, nil, nil)
	require.NoError(t, err)

	bm.WithDependencies(&hooks.HookExecutionDependencyContainer{})

	_, err = bm.RegisterHooks(log.NewLogger(log.Options{}))
	require.NoError(t, err)

	hooks := bm.GetHooks()
	require.Len(t, hooks, 1)
	require.Equal(t, "hooks/https/test_hook.py", hooks[0].GetName())
}

func TestHookErrorsSummary(t *testing.T) {
	bm := &BasicModule{
		Name:  "test",
		Path:  "/mod/path",
		state: &moduleState{hookErrors: make(map[string]error)},
	}
	bm.SaveHookError("hooks/https/test.py", errors.New("fail"))

	summary := bm.GetHookErrorsSummary()
	require.Contains(t, summary, "hooks/https/test.py: fail")
}

type noopConfigManager struct{}

func (*noopConfigManager) SaveConfigValues(string, utils.Values) error { return nil }

type noopValuesGetter struct{}

func (*noopValuesGetter) GetValues(bool) utils.Values       { return utils.Values{} }
func (*noopValuesGetter) GetConfigValues(bool) utils.Values { return utils.Values{} }

func TestRunHookByNameWorksWithNormalizedName(t *testing.T) {
	modulePath := "/tmp/modules/console/v1.2.3"
	hookPath := filepath.Join(modulePath, "hooks/test.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o755))

	err := os.WriteFile(hookPath, []byte(`#!/bin/sh
if [ "$1" = "--config" ]; then
  echo '{"configVersion":"v1","onStartup":10}'
  exit 0
fi
exit 0
`), 0o755)
	require.NoError(t, err)

	defer os.RemoveAll("/tmp/modules")

	bm, err := NewBasicModule("example", modulePath, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger(log.Options{})
	storage := metric_storage.NewMetricStorage(context.TODO(), "addon_operator_", false, logger)

	bm.WithDependencies(&hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: storage,
		MetricStorage:      storage,
		KubeObjectPatcher:  objectpatch.NewObjectPatcher(nil, logger),
		KubeConfigManager:  &noopConfigManager{},
		GlobalValuesGetter: &noopValuesGetter{},
	})
	_, err = bm.RegisterHooks(logger)
	require.NoError(t, err)

	for _, h := range bm.GetHooks() {
		h.WithHookController(controller.NewHookController())
	}
	for _, h := range bm.GetHooks() {
		t.Logf("Hook: %s", h.GetName())
		t.Logf("  HookController nil? %v", h.GetHookController() == nil)
	}

	_, _, err = bm.RunHookByName(context.TODO(), "hooks/test.sh", sh_op_types.OnStartup, nil, map[string]string{})
	require.NoError(t, err)

	found := false
	for _, h := range bm.GetHooks() {
		if h.GetName() == "hooks/test.sh" {
			found = true
		}
	}
	require.True(t, found, "hook 'hooks/test.sh' should be registered")
}

func TestHooksOutsideHooksDirAreNotRegistered(t *testing.T) {
	modulePath := "/tmp/modules/example/v1.2.3"
	hookPath := filepath.Join(modulePath, "customHooks", "invalid_hook.sh")

	err := os.MkdirAll(filepath.Dir(hookPath), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(hookPath, []byte(`#!/bin/sh
if [ "$1" = "--config" ]; then
  echo '{"configVersion":"v1","onStartup":10}'
  exit 0
fi
exit 0
`), 0o755)
	require.NoError(t, err)
	defer os.RemoveAll("/tmp/modules")

	bm, err := NewBasicModule("example", modulePath, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger(log.Options{})
	storage := metric_storage.NewMetricStorage(context.TODO(), "addon_operator_", false, logger)

	bm.WithLogger(logger)
	bm.WithDependencies(&hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: storage,
		MetricStorage:      storage,
		KubeObjectPatcher:  objectpatch.NewObjectPatcher(nil, logger),
		KubeConfigManager:  &noopConfigManager{},
		GlobalValuesGetter: &noopValuesGetter{},
	})

	_, err = bm.RegisterHooks(logger)
	require.NoError(t, err)

	hooks := bm.GetHooks()
	for _, h := range hooks {
		t.Logf("Registered: %s", h.GetName())
	}
	require.Empty(t, hooks, "Hooks outside 'hooks/' dir should not be registered")
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
