package modules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	sdkutils "github.com/deckhouse/module-sdk/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
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

	_, err = bm.RegisterHooks(log.NewLogger())
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

	logger := log.NewLogger()
	storage := metricsstorage.NewMetricStorage(
		metricsstorage.WithLogger(logger),
	)

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

	logger := log.NewLogger()
	storage := metricsstorage.NewMetricStorage(
		metricsstorage.WithLogger(logger),
	)

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

func stubDeps(logger *log.Logger) *hooks.HookExecutionDependencyContainer {
	st := metricsstorage.NewMetricStorage(
		metricsstorage.WithLogger(logger),
	)
	return &hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: st,
		MetricStorage:      st,
		KubeObjectPatcher:  objectpatch.NewObjectPatcher(nil, logger),
		GlobalValuesGetter: &noopValuesGetter{},
		KubeConfigManager:  &noopConfigManager{},
	}
}

func TestRunEnabledScriptTrue(t *testing.T) {
	tmpModuleDir := t.TempDir()
	enabledPath := filepath.Join(tmpModuleDir, "enabled")

	require.NoError(t, os.WriteFile(enabledPath, []byte(`#!/bin/sh
echo "true" > "$MODULE_ENABLED_RESULT"
exit 0
`), 0o755))

	bm, err := NewBasicModule("example", tmpModuleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger()
	bm.WithLogger(logger)
	bm.WithDependencies(stubDeps(logger))

	ok, err := bm.RunEnabledScript(context.TODO(), t.TempDir(), nil, map[string]string{})
	require.NoError(t, err)
	require.True(t, ok, "Module should be enabled")

	require.NotNil(t, bm.GetEnabledScriptResult())
	require.True(t, *bm.GetEnabledScriptResult())
	r := bm.GetEnabledScriptReason()
	require.Nil(t, r)
}

func TestRunEnabledScriptFalseWithReason(t *testing.T) {
	tmpModuleDir := t.TempDir()
	enabledPath := filepath.Join(tmpModuleDir, "enabled")

	require.NoError(t, os.WriteFile(enabledPath, []byte(`#!/bin/sh
echo "false" > "$MODULE_ENABLED_RESULT"
echo "Kubernetes version is too low" > "$MODULE_ENABLED_REASON"
exit 0
`), 0o755))

	bm, err := NewBasicModule("example", tmpModuleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger()
	bm.WithLogger(logger)
	bm.WithDependencies(stubDeps(logger))

	ok, err := bm.RunEnabledScript(context.TODO(), t.TempDir(), nil, map[string]string{})
	require.NoError(t, err)
	require.False(t, ok, "Module should be disabled")

	require.NotNil(t, bm.GetEnabledScriptResult())
	require.False(t, *bm.GetEnabledScriptResult())

	erGetter := bm.GetEnabledScriptReason()
	require.NotNil(t, erGetter)
	require.Equal(t, "Kubernetes version is too low", *erGetter)
}

// TestHasReadinessResetOnRetry tests that hasReadiness is reset when searching for batch hooks.
// This prevents "multiple readiness hooks found" error when registration is retried after
// an error that occurred after hasReadiness was set (e.g., in AssembleEnvironmentForModule).
func TestHasReadinessResetOnRetry(t *testing.T) {
	tmpModuleDir := t.TempDir()

	bm, err := NewBasicModule("test-readiness-reset", tmpModuleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger()
	bm.WithLogger(logger)
	bm.WithDependencies(stubDeps(logger))

	// Simulate a state where hasReadiness was set to true from a previous failed attempt
	bm.hasReadiness = true

	// searchModuleBatchHooks should reset hasReadiness before searching for hooks
	// Even though hooks.registered is false, hasReadiness was left as true
	// Without the fix, this would cause issues on retry
	_, err = bm.RegisterHooks(logger)
	require.NoError(t, err, "RegisterHooks should not fail even if hasReadiness was true from previous attempt")

	// After successful registration, hasReadiness should be false (no hooks found in empty dir)
	require.False(t, bm.hasReadiness, "hasReadiness should be false after registering empty module")
}

// TestSearchModuleBatchHooksResetsReadiness verifies that searchModuleBatchHooks
// resets hasReadiness at the start, preventing false "multiple readiness hooks found"
// errors when retrying after a previous failed attempt.
//
// To reproduce the original bug WITHOUT the fix:
// 1. Comment out "bm.hasReadiness = false" in searchModuleBatchHooks()
// 2. Run this test - it will fail with "multiple readiness hooks found" error
func TestSearchModuleBatchHooksResetsReadiness(t *testing.T) {
	tmpModuleDir := t.TempDir()

	bm, err := NewBasicModule("test-readiness-reset-batch", tmpModuleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	logger := log.NewLogger()
	bm.WithLogger(logger)

	// Simulate state after a failed registration attempt:
	// - hasReadiness was set to true during searchModuleBatchHooks
	// - but hooks.registered stayed false because error occurred later
	bm.hasReadiness = true
	// hooks.registered is false by default

	// Call searchModuleBatchHooks directly (via RegisterHooks -> searchModuleHooks)
	// Without the fix, this would return "multiple readiness hooks found" error
	// because hasReadiness is already true from the "previous" attempt
	batchHooks, err := bm.searchModuleBatchHooks()
	require.NoError(t, err, "searchModuleBatchHooks should reset hasReadiness and not fail")

	// No hooks in empty dir
	require.Empty(t, batchHooks)

	// hasReadiness should be false (no readiness hooks found in empty dir)
	require.False(t, bm.hasReadiness, "hasReadiness should be false after searching empty hooks dir")
}
