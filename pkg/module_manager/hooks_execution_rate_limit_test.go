package module_manager_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	taskmeta "github.com/flant/addon-operator/pkg/task"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	moduledelete "github.com/flant/addon-operator/pkg/task/tasks/module-delete"
	modulehookrun "github.com/flant/addon-operator/pkg/task/tasks/module-hook-run"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	kemtypes "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	shelloperator "github.com/flant/shell-operator/pkg/shell-operator"
	shtask "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const executionParallelQueue = "/modules/example/events"
const executionMarkerType shtask.TaskType = "ExecutionTestMarker"

// Pause entry into rate-limit waiting while the module changes state.
type rateLimitBarrier struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	unblock sync.Once
}

func (c *rateLimitBarrier) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.entered)
		<-c.release
	})
	return c.Context.Done()
}

func (c *rateLimitBarrier) Release() {
	c.unblock.Do(func() { close(c.release) })
}

func newRateLimitedExecutionFixture(t *testing.T, interval time.Duration) *executionFixture {
	t.Helper()
	config := fmt.Sprintf(`{"configVersion":"v1","settings":{"executionMinInterval":%q,"executionBurst":1},"kubernetes":[{"name":"config","queue":%q,"includeSnapshotsFrom":["config"],"apiVersion":"v1","kind":"ConfigMap","namespace":{"nameSelector":{"matchNames":["default"]}},"nameSelector":{"matchNames":["existing-config"]},"jqFilter":".metadata.name"}]}`, interval.String(), executionParallelQueue)
	f := newExecutionFixtureWithConfig(t, config)
	f.enableHooks(t)
	return f
}

func newExecutionHookTask(f *executionFixture, queueName string) shtask.Task {
	hook := f.module.GetHooks()[0]
	bc := bindingcontext.BindingContext{Binding: "config"}
	bc.Metadata.BindingType = htypes.OnKubernetesEvent
	bc.Metadata.IncludeAllSnapshots = true
	return shtask.NewTask(taskmeta.ModuleHookRun).WithQueueName(queueName).WithMetadata(taskmeta.HookMetadata{
		ModuleName: f.module.GetName(), HookName: hook.GetName(), Binding: "config", BindingType: htypes.OnKubernetesEvent,
		KubernetesBindingId: hook.GetHookConfig().OnKubernetesEvents[0].Monitor.Metadata.MonitorId,
		BindingContext:      []bindingcontext.BindingContext{bc},
	})
}

type waitingExecutionHook struct {
	ctx     context.Context
	cancel  context.CancelFunc
	barrier *rateLimitBarrier
	queues  *queue.TaskQueueSet
	service *taskqueue.Service
	task    shtask.Task
	result  chan queue.TaskResult
	marker  chan struct{}
}

func startWaitingExecutionHook(t *testing.T, f *executionFixture) *waitingExecutionHook {
	t.Helper()
	require.NoError(t, f.module.GetHooks()[0].RateLimitWait(f.ctx))
	ctx, cancel := context.WithCancel(f.ctx)
	barrier := &rateLimitBarrier{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
	queues := queue.NewTaskQueueSet().WithMetricStorage(f.metrics).WithLogger(log.NewNop())
	queues.WithContext(ctx)
	service := taskqueue.NewService(ctx, &taskqueue.ServiceConfig{Engine: &shelloperator.ShellOperator{TaskQueues: queues}}, log.NewNop())
	waiter := &waitingExecutionHook{ctx: ctx, cancel: cancel, barrier: barrier, queues: queues, service: service, task: newExecutionHookTask(f, executionParallelQueue), result: make(chan queue.TaskResult, 1), marker: make(chan struct{})}
	queues.NewNamedQueue(executionParallelQueue, func(ctx context.Context, task shtask.Task) queue.TaskResult {
		if task.GetType() == executionMarkerType {
			close(waiter.marker)
			return queue.TaskResult{Status: queue.Success}
		}
		result := modulehookrun.NewTask(task, false, nil, f.manager, f.metrics, service, log.NewNop()).Handle(ctx)
		waiter.result <- result
		return result
	}, queue.WithLogger(log.NewNop()))
	parallel := queues.GetByName(executionParallelQueue)
	parallel.AddLast(waiter.task)
	parallel.Start(barrier)
	t.Cleanup(func() {
		cancel()
		barrier.Release()
		queues.Stop()
		require.Eventually(t, func() bool {
			for _, q := range queues.Queues.List() {
				if q.GetStatusType() != queue.QueueStatusStop {
					return false
				}
			}
			return true
		}, 5*time.Second, time.Millisecond, "queue workers did not stop")
	})
	waitExecutionSignal(t, barrier.entered)
	return waiter
}

func waitExecutionResult(t *testing.T, result <-chan queue.TaskResult) queue.TaskResult {
	t.Helper()
	select {
	case res := <-result:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("task did not finish")
		return queue.TaskResult{}
	}
}

func (w *waitingExecutionHook) deleteModuleOnMain(t *testing.T, f *executionFixture) {
	t.Helper()
	deleted := make(chan queue.TaskResult, 1)
	marker := make(chan struct{})
	w.queues.NewNamedQueue("main", func(ctx context.Context, task shtask.Task) queue.TaskResult {
		if task.GetType() == executionMarkerType {
			close(marker)
			return queue.TaskResult{Status: queue.Success}
		}
		result := moduledelete.NewTask(task, f.manager, f.metrics, w.service, log.NewNop()).Handle(ctx)
		deleted <- result
		return result
	}, queue.WithLogger(log.NewNop()))
	main := w.queues.GetMain()
	main.AddLast(
		shtask.NewTask(taskmeta.ModuleDelete).WithQueueName("main").WithMetadata(taskmeta.HookMetadata{ModuleName: f.module.GetName()}),
		shtask.NewTask(executionMarkerType).WithQueueName("main"),
	)
	main.Start(w.ctx)
	waitExecutionSignal(t, marker)
	require.Equal(t, queue.Success, waitExecutionResult(t, deleted).Status)
}

func TestRateLimitedModuleHookDoesNotBlockDisable(t *testing.T) {
	f := newRateLimitedExecutionFixture(t, time.Hour)
	waiter := startWaitingExecutionHook(t, f)
	disabled := make(chan struct{})
	go func() {
		f.manager.DisableModuleHooks(f.module.GetName())
		close(disabled)
	}()
	waitExecutionSignal(t, disabled)
	require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
	require.NoFileExists(t, f.marker)

	waiter.cancel()
	waiter.barrier.Release()
	require.Equal(t, queue.Repeat, waitExecutionResult(t, waiter.result).Status)
}

func TestRateLimitedModuleHookDoesNotBlockMainQueueDeletion(t *testing.T) {
	f := newRateLimitedExecutionFixture(t, time.Hour)
	waiter := startWaitingExecutionHook(t, f)
	waiter.deleteModuleOnMain(t, f)
	require.Equal(t, modules.Startup, f.module.GetPhase())
	require.Empty(t, f.module.GetHooks())
	require.NoFileExists(t, f.marker)

	waiter.cancel()
	waiter.barrier.Release()
	require.Equal(t, queue.Repeat, waitExecutionResult(t, waiter.result).Status)
}

func TestModuleHookWaitingForRateLimitDoesNotRunAfterReenable(t *testing.T) {
	f := newRateLimitedExecutionFixture(t, time.Millisecond)
	oldHook := f.module.GetHooks()[0]
	waiter := startWaitingExecutionHook(t, f)
	waiter.deleteModuleOnMain(t, f)
	f.enableHooks(t)
	require.NotSame(t, oldHook, f.module.GetHooks()[0])
	waiter.queues.GetByName(executionParallelQueue).AddLast(shtask.NewTask(executionMarkerType).WithQueueName(executionParallelQueue))

	waiter.barrier.Release()
	require.Equal(t, queue.Success, waitExecutionResult(t, waiter.result).Status)
	waitExecutionSignal(t, waiter.marker)
	require.NoFileExists(t, f.marker)
	require.Equal(t, modules.Ready, f.module.GetPhase())
	require.Len(t, f.module.GetHooks()[0].GetHookController().KubernetesSnapshots()["config"], 1)
}

func TestCanceledModuleHookKeepsSynchronizationPending(t *testing.T) {
	for _, atEntry := range []bool{true, false} {
		t.Run(fmt.Sprintf("atEntry=%t", atEntry), func(t *testing.T) {
			f := newRateLimitedExecutionFixture(t, time.Hour)
			task := newExecutionHookTask(f, executionParallelQueue)
			hm := taskmeta.HookMetadataAccessor(task)
			hm.BindingContext[0].Type = kemtypes.TypeSynchronization
			hm.ExecuteOnSynchronization = true
			hm.WaitForSynchronization = true
			task.UpdateMetadata(hm)
			f.module.Synchronization().QueuedForBinding(hm)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			var result queue.TaskResult
			if atEntry {
				cancel()
				result = modulehookrun.NewTask(task, false, nil, f.manager, f.metrics, nil, log.NewNop()).Handle(ctx)
			} else {
				require.NoError(t, f.module.GetHooks()[0].RateLimitWait(f.ctx))
				barrier := &rateLimitBarrier{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
				defer barrier.Release()
				finished := make(chan queue.TaskResult, 1)
				go func() {
					finished <- modulehookrun.NewTask(task, false, nil, f.manager, f.metrics, nil, log.NewNop()).Handle(barrier)
				}()
				waitExecutionSignal(t, barrier.entered)
				cancel()
				barrier.Release()
				result = waitExecutionResult(t, finished)
			}
			require.Equal(t, queue.Repeat, result.Status)
			require.False(t, f.module.Synchronization().IsCompleted())
			require.NoFileExists(t, f.marker)
		})
	}
}

func TestDisableWaitsForRunningModuleHookBeforeRemovingSnapshots(t *testing.T) {
	f := newRateLimitedExecutionFixture(t, time.Millisecond)
	blocker := &blockingResources{started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(blocker.release) })
	service := taskqueue.NewService(f.ctx, &taskqueue.ServiceConfig{Engine: &shelloperator.ShellOperator{TaskQueues: queue.NewTaskQueueSet()}}, log.NewNop())
	runDone := make(chan queue.TaskResult, 1)
	go func() {
		runDone <- modulehookrun.NewTask(newExecutionHookTask(f, "main"), false, blocker, f.manager, f.metrics, service, log.NewNop()).Handle(f.ctx)
	}()
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
		t.Fatal("hooks were disabled while ModuleHookRun still needs their snapshots")
	case <-time.After(100 * time.Millisecond):
	}
	require.Len(t, f.module.GetHooks()[0].GetHookController().KubernetesSnapshots()["config"], 1)
	release.Do(func() { close(blocker.release) })
	require.Equal(t, queue.Success, waitExecutionResult(t, runDone).Status)
	waitExecutionSignal(t, disableDone)
	contextJSON, err := os.ReadFile(f.marker)
	require.NoError(t, err)
	require.Contains(t, string(contextJSON), "existing-config")
	require.Equal(t, modules.HooksDisabled, f.module.GetPhase())
}
