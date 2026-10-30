package module_manager_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/stretchr/testify/require"

	mockhelmres "github.com/flant/addon-operator/pkg/helm_resources_manager/test/mock"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	taskmeta "github.com/flant/addon-operator/pkg/task"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	moduledelete "github.com/flant/addon-operator/pkg/task/tasks/module-delete"
	modulehookrun "github.com/flant/addon-operator/pkg/task/tasks/module-hook-run"
	modulerun "github.com/flant/addon-operator/pkg/task/tasks/module-run"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/fake"
	"github.com/flant/kube-client/manifest"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	kubeeventsmanager "github.com/flant/shell-operator/pkg/kube_events_manager"
	shelloperator "github.com/flant/shell-operator/pkg/shell-operator"
	shtask "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type executionValues struct{}

func (executionValues) GetValues(bool) utils.Values       { return utils.Values{} }
func (executionValues) GetConfigValues(bool) utils.Values { return utils.Values{} }

type executionFixture struct {
	ctx      context.Context
	module   *modules.BasicModule
	manager  *module_manager.ModuleManager
	metrics  metricsstorage.Storage
	marker   string
	monitors kubeeventsmanager.KubeEventsManager
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logger := log.NewNop()
	metrics := metricsstorage.NewMetricStorage(metricsstorage.WithLogger(logger))
	dir := t.TempDir()
	marker := filepath.Join(dir, "binding-context.json")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "hooks"), 0o755))
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--config" ]; then
    echo '{"configVersion":"v1","beforeHelm":10,"afterDeleteHelm":10,"kubernetes":[{"name":"config","apiVersion":"v1","kind":"ConfigMap","namespace":{"nameSelector":{"matchNames":["default"]}},"nameSelector":{"matchNames":["existing-config"]},"jqFilter":".metadata.name"}]}'
    exit 0
fi
cat "$BINDING_CONTEXT_PATH" > %q
`, marker)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hooks", "config.sh"), []byte(script), 0o755))

	module, err := modules.NewBasicModule("example", dir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)
	module.WithLogger(logger)
	module.WithDependencies(&hooks.HookExecutionDependencyContainer{
		MetricStorage:      metrics,
		HookMetricsStorage: metrics,
		GlobalValuesGetter: executionValues{},
	})

	cluster := fake.NewFakeCluster(fake.ClusterVersionV121)
	cluster.CreateNs("default")
	require.NoError(t, cluster.Create("default", manifest.MustFromYAML(`apiVersion: v1
kind: ConfigMap
metadata:
  name: existing-config
`)))
	monitors := kubeeventsmanager.NewKubeEventsManager(ctx, cluster.Client, logger)
	monitors.WithMetricStorage(metrics)
	manager := module_manager.NewModuleManagerForExecutionTest(t, ctx, module, module_manager.ModuleManagerDependencies{
		MetricStorage:        metrics,
		HookMetricStorage:    metrics,
		KubeEventsManager:    monitors,
		HelmResourcesManager: &mockhelmres.MockHelmResourcesManager{},
	})
	return &executionFixture{ctx: ctx, module: module, manager: manager, metrics: metrics, marker: marker, monitors: monitors}
}

func (f *executionFixture) enableHooks(t *testing.T) {
	t.Helper()
	require.NoError(t, f.manager.RegisterModuleHooks(f.module, nil))
	require.NoError(t, f.manager.HandleModuleEnableKubernetesBindings(f.ctx, f.module.GetName(), nil))
	require.Len(t, f.module.GetHooks()[0].GetHookController().KubernetesSnapshots()["config"], 1)
	f.module.SetPhase(modules.Ready)
}

func (f *executionFixture) runTask() queue.TaskResult {
	shellTask := shtask.NewTask(taskmeta.ModuleRun).WithQueueName("main").WithMetadata(taskmeta.HookMetadata{ModuleName: f.module.GetName()})
	return modulerun.NewTask(shellTask, false, f.manager, f.metrics, nil, log.NewNop()).Handle(f.ctx)
}

func TestModuleRunAfterDisableDoesNotRunHooks(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmt.Sprintf("controllersReady=%t", registered), func(t *testing.T) {
			f := newExecutionFixture(t)
			if registered {
				f.enableHooks(t)
			}
			f.manager.DisableModuleHooks(f.module.GetName())
			require.Equal(t, modules.HooksDisabled, f.module.GetPhase())

			for range 2 {
				require.Equal(t, queue.Success, f.runTask().Status)
				require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
				require.NoFileExists(t, f.marker)
			}
		})
	}
}

func TestQueuedModuleHookAfterDisableDoesNotRun(t *testing.T) {
	f := newExecutionFixture(t)
	f.enableHooks(t)
	f.manager.DisableModuleHooks(f.module.GetName())

	for _, binding := range []htypes.BindingType{htypes.OnKubernetesEvent, htypes.Schedule} {
		shellTask := shtask.NewTask(taskmeta.ModuleHookRun).WithQueueName("parallel").WithMetadata(taskmeta.HookMetadata{
			ModuleName: f.module.GetName(), HookName: "hooks/config.sh", BindingType: binding,
		})
		result := modulehookrun.NewTask(shellTask, false, nil, f.manager, f.metrics, nil, log.NewNop()).Handle(f.ctx)
		require.Equal(t, queue.Success, result.Status)
		require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
		require.NoFileExists(t, f.marker)
	}
}

type blockingResources struct {
	mockhelmres.MockHelmResourcesManager
	started chan struct{}
	release chan struct{}
}

func (m *blockingResources) PauseMonitor(string) {
	close(m.started)
	<-m.release
}

func TestDisableWaitsForRunningModuleBeforeRemovingSnapshots(t *testing.T) {
	f := newExecutionFixture(t)
	f.enableHooks(t)
	blocker := &blockingResources{started: make(chan struct{}), release: make(chan struct{})}
	f.manager.GetDependencies().HelmResourcesManager = blocker
	released := false
	defer func() {
		if !released {
			close(blocker.release)
		}
	}()

	runDone := make(chan queue.TaskResult, 1)
	go func() { runDone <- f.runTask() }()
	waitExecutionSignal(t, blocker.started)

	disableStarted := make(chan struct{})
	disableDone := make(chan struct{})
	go func() {
		close(disableStarted)
		f.manager.DisableModuleHooks(f.module.GetName())
		close(disableDone)
	}()
	waitExecutionSignal(t, disableStarted)
	select {
	case <-disableDone:
		t.Fatal("hooks were disabled while ModuleRun still needs their snapshots")
	case <-time.After(100 * time.Millisecond):
	}
	require.Len(t, f.module.GetHooks()[0].GetHookController().KubernetesSnapshots()["config"], 1)
	close(blocker.release)
	released = true

	select {
	case result := <-runDone:
		require.Equal(t, queue.Success, result.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("ModuleRun did not finish")
	}
	waitExecutionSignal(t, disableDone)
	require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
	contextJSON, err := os.ReadFile(f.marker)
	require.NoError(t, err)
	require.Contains(t, string(contextJSON), "existing-config")
	monitorID := f.module.GetHooks()[0].GetHookConfig().OnKubernetesEvents[0].Monitor.Metadata.MonitorId
	require.False(t, f.monitors.HasMonitor(monitorID))
	require.Equal(t, queue.Success, f.runTask().Status)
	require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
}

func waitExecutionSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not reach the expected barrier")
	}
}

func TestModuleDeleteAfterDisableWaitsForRestart(t *testing.T) {
	f := newExecutionFixture(t)
	f.enableHooks(t)
	f.manager.DisableModuleHooks(f.module.GetName())
	shellTask := shtask.NewTask(taskmeta.ModuleDelete).WithQueueName("main").WithMetadata(taskmeta.HookMetadata{ModuleName: f.module.GetName()})
	result := moduledelete.NewTask(shellTask, f.manager, f.metrics, nil, log.NewNop()).Handle(f.ctx)
	require.Equal(t, queue.Fail, result.Status)
	require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
	require.NoFileExists(t, f.marker)
}

func TestModuleDeleteKeepsSnapshotsAndAllowsReenable(t *testing.T) {
	f := newExecutionFixture(t)
	f.enableHooks(t)
	queues := taskqueue.NewService(f.ctx, &taskqueue.ServiceConfig{
		Engine: &shelloperator.ShellOperator{TaskQueues: queue.NewTaskQueueSet()},
	}, log.NewNop())
	shellTask := shtask.NewTask(taskmeta.ModuleDelete).WithQueueName("main").WithMetadata(taskmeta.HookMetadata{ModuleName: f.module.GetName()})
	result := moduledelete.NewTask(shellTask, f.manager, f.metrics, queues, log.NewNop()).Handle(f.ctx)
	require.Equal(t, queue.Success, result.Status)
	require.Equal(t, modules.Startup, f.module.GetPhase())
	require.Empty(t, f.module.GetHooks())
	contextJSON, err := os.ReadFile(f.marker)
	require.NoError(t, err)
	require.Contains(t, string(contextJSON), "existing-config")
	f.enableHooks(t)
	require.Equal(t, queue.Success, f.runTask().Status)
	require.Equal(t, modules.Ready, f.module.GetPhase())
}
