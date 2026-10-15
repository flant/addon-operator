package module_manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/fake"
	"github.com/flant/shell-operator/pkg/hook/controller"
	kubeeventsmanager "github.com/flant/shell-operator/pkg/kube_events_manager"
)

// flakyMonitorSource wraps the real KubeEventsManager and fails AddMonitor for
// the given kind a limited number of times - the transient apiserver error that
// aborts EnableKubernetesBindings for a single hook.
type flakyMonitorSource struct {
	kubeeventsmanager.KubeEventsManager

	mu       sync.Mutex
	failKind string
	failures int
}

func (f *flakyMonitorSource) AddMonitor(config *kubeeventsmanager.MonitorConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if config.Kind == f.failKind && f.failures > 0 {
		f.failures--
		return errors.New("transient: apiserver unavailable")
	}

	return f.KubeEventsManager.AddMonitor(config)
}

func writeShellHook(t *testing.T, hooksDir, name, kind string) {
	t.Helper()

	script := `#!/bin/sh
if [ "$1" = "--config" ]; then
  echo '{"configVersion":"v1","kubernetes":[{"name":"objects","apiVersion":"v1","kind":"` + kind + `","executeHookOnEvent":["Added"]}]}'
  exit 0
fi
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, name), []byte(script), 0o755))
}

// TestHandleModuleEnableKubernetesBindings_RetryAfterPartialFailure encodes the
// acceptance scenario for the lost-Synchronization incident: on the first pass
// one hook cannot start its monitor, the other hooks still emit their
// Synchronization contexts and the error names the failing hook; on the retry
// pass every hook emits its Synchronization context again, so nothing is lost
// and the module can proceed.
// The failing hook sorts FIRST: with the old fail-fast loop in
// HandleModuleEnableKubernetesBindings the pass-1 assert would then see zero
// contexts, so a revert of the best-effort aggregation fails the test too.
// Requires shell-operator with idempotent EnableKubernetesBindings (>= v1.20.2).
func TestHandleModuleEnableKubernetesBindings_RetryAfterPartialFailure(t *testing.T) {
	logger := log.NewNop()

	moduleDir := t.TempDir()
	hooksDir := filepath.Join(moduleDir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	writeShellHook(t, hooksDir, "0-fail.sh", "ConfigMap") // the failing one, sorts first
	writeShellHook(t, hooksDir, "a.sh", "Pod")
	writeShellHook(t, hooksDir, "b.sh", "Pod")

	fc := fake.NewFakeCluster(fake.ClusterVersionV121)
	mgr := kubeeventsmanager.NewKubeEventsManager(context.Background(), fc.Client, logger)
	source := &flakyMonitorSource{KubeEventsManager: mgr, failKind: "ConfigMap", failures: 1}

	bm, err := modules.NewBasicModule("retry-sync", moduleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)
	// Registration dereferences the container for shell hooks; an empty one is enough.
	bm.WithDependencies(&hooks.HookExecutionDependencyContainer{})

	hks, err := bm.RegisterHooks(logger)
	require.NoError(t, err)
	require.Len(t, hks, 3)

	for _, hk := range hks {
		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(hk.GetHookConfig().OnKubernetesEvents, source, logger)
		hk.WithHookController(hookCtrl)
	}
	bm.SetHooksControllersReady()

	mm := NewModuleManager(context.Background(), &ModuleManagerConfig{}, logger)
	mm.modules.Add(bm)

	synced := make(map[string]int)
	collect := func(mh *hooks.ModuleHook, _ controller.BindingExecutionInfo) {
		synced[filepath.Base(mh.GetName())]++
	}

	// Pass 1: the first hook fails, the remaining ones must still emit their
	// Synchronization contexts (with fail-fast the map would be empty).
	err = mm.HandleModuleEnableKubernetesBindings(context.Background(), "retry-sync", collect)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0-fail.sh", "the error must name the failing hook")
	assert.Equal(t, map[string]int{"a.sh": 1, "b.sh": 1}, synced,
		"hooks after the failing one must emit Synchronization contexts on the failing pass")

	// Pass 2 (ModuleRun retry): every hook emits its Synchronization context again.
	clear(synced)
	err = mm.HandleModuleEnableKubernetesBindings(context.Background(), "retry-sync", collect)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"0-fail.sh": 1, "a.sh": 1, "b.sh": 1}, synced,
		"retry must emit Synchronization for previously succeeded hooks too (idempotent Enable)")
}
