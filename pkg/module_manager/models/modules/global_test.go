package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/utils"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	metric_storage "github.com/flant/shell-operator/pkg/metric_storage"
)

func TestRunHookByNameWorksWithNormalizedCustomPathName(t *testing.T) {
	globalHooksDir := "/tmp/global/testGlobalOutside"
	hookPath := filepath.Join(globalHooksDir, "customHooks", "custom_hook.sh")

	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o755))

	err := os.WriteFile(hookPath, []byte(`#!/bin/sh
if [ "$1" = "--config" ]; then
  echo '{"configVersion":"v1","onStartup":10}'
  exit 0
fi
exit 0
`), 0o755)
	require.NoError(t, err)
	defer os.RemoveAll("/tmp/global")
	logger := log.NewLogger()
	storage := metric_storage.NewMetricStorage(context.TODO(), "addon_operator_", false, logger)
	gm, err := NewGlobalModule(
		globalHooksDir,
		utils.Values{},
		&hooks.HookExecutionDependencyContainer{
			HookMetricsStorage: storage,
			MetricStorage:      storage,
			KubeObjectPatcher:  objectpatch.NewObjectPatcher(nil, logger),
			KubeConfigManager:  &noopConfigManager{},
			GlobalValuesGetter: &noopValuesGetter{},
		},
		nil, nil, false,
	)
	require.NoError(t, err)

	_, err = gm.RegisterHooks()
	require.NoError(t, err)

	found := false
	for _, h := range gm.GetHooks() {
		if h.GetName() == "customHooks/custom_hook.sh" {
			found = true
		}
	}
	require.True(t, found, "hook 'custom_hook.sh' should be registered")
}
