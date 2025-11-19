package globalhookenablekubernetesbindings

import (
	"context"
	"log/slog"
	"path"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/gofrs/uuid/v5"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "global-hook-enable-kubernetes-bindings"
)

// TaskDependencies provides access to services needed by task handlers.
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler returns a factory function that creates task handlers.
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			svc.GetMetricStorage(),
			svc.GetQueueService(),
			logger.Named("global-hook-enable-kubernetes-bindings"),
		)
	}
}

// Task handles enabling kubernetes bindings for global hooks.
type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	metricStorage metricsstorage.Storage
	queueService  *taskqueue.Service
	logger        *log.Logger
}

// NewTask creates a new task handler for enabling kubernetes bindings for global hooks.
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	metricStorage metricsstorage.Storage,
	queueService *taskqueue.Service,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:     shellTask,
		moduleManager: moduleManager,
		metricStorage: metricStorage,
		queueService:  queueService,
		logger:        logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res queue.TaskResult
	s.logger.Debug("Global hook enable kubernetes bindings")

	hm := task.HookMetadataAccessor(s.shellTask)
	globalHook := s.moduleManager.GetGlobalHook(hm.HookName)

	mainSyncTasks := make([]sh_task.Task, 0)
	parallelSyncTasks := make([]sh_task.Task, 0)
	parallelSyncTasksToWait := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	newLogLabels := utils.MergeLabels(s.shellTask.GetLogLabels())
	delete(newLogLabels, pkg.LogKeyTaskID)

	err := s.moduleManager.HandleGlobalEnableKubernetesBindings(ctx, hm.HookName, func(hook *hooks.GlobalHook, info controller.BindingExecutionInfo) {
		taskLogLabels := utils.MergeLabels(s.shellTask.GetLogLabels(), map[string]string{
			pkg.LogKeyBinding:  htypes.OnKubernetesEvent.String() + "Synchronization",
			pkg.LogKeyHook:     hook.GetName(),
			pkg.LogKeyHookType: "global",
			pkg.LogKeyQueue:    info.QueueName,
		})
		if len(info.BindingContext) > 0 {
			taskLogLabels[pkg.LogKeyBindingName] = info.BindingContext[0].Binding
		}
		delete(taskLogLabels, pkg.LogKeyTaskID)

		kubernetesBindingID := uuid.Must(uuid.NewV4()).String()
		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(taskLogLabels).
			WithQueueName(info.QueueName).
			WithMetadata(task.HookMetadata{
				EventDescription:         hm.EventDescription,
				HookName:                 hook.GetName(),
				BindingType:              htypes.OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				ReloadAllOnValuesChanges: false, // Ignore global values changes in global Synchronization tasks.
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
			}).
			WithCompactionID(hook.GetName())
		newTask.WithQueuedAt(queuedAt)

		if info.QueueName == s.shellTask.GetQueueName() {
			// Ignore "waitForSynchronization: false" for hooks in the main queue.
			// There is no way to not wait for these hooks.
			mainSyncTasks = append(mainSyncTasks, newTask)
		} else {
			// Do not wait for parallel hooks on "waitForSynchronization: false".
			if info.KubernetesBinding.WaitForSynchronization {
				parallelSyncTasksToWait = append(parallelSyncTasksToWait, newTask)
			} else {
				parallelSyncTasks = append(parallelSyncTasks, newTask)
			}
		}
	})
	if err != nil {
		hookLabel := path.Base(globalHook.GetPath())
		// TODO use separate metric, as in shell-operator?
		s.metricStorage.CounterAdd(metrics.GlobalHookErrorsTotal, 1.0, map[string]string{
			pkg.MetricKeyHook:       hookLabel,
			pkg.MetricKeyBinding:    "GlobalEnableKubernetesBindings",
			pkg.MetricKeyQueue:      s.shellTask.GetQueueName(),
			pkg.MetricKeyActivation: converge.OperatorStartup.String(),
		})
		s.logger.Error("Global hook enable kubernetes bindings failed, requeue task to retry after delay.",
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(err))
		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(queuedAt)
		res.Status = queue.Fail
		return res
	}
	// Substitute current task with Synchronization tasks for the main queue.
	// Other Synchronization tasks are queued into specified queues.
	// Informers can be started now — their events will be added to the queue tail.
	s.logger.Debug("Global hook enable kubernetes bindings success")

	// "Wait" tasks are queued first
	for _, tsk := range parallelSyncTasksToWait {
		if err := s.queueService.AddLastTaskToQueue(tsk.GetQueueName(), tsk); err != nil {
			s.logger.Error("Queue is not created while run GlobalHookEnableKubernetesBindings parallel wait task!",
				slog.String("queue", tsk.GetQueueName()))

			continue
		}

		// Skip state creation if WaitForSynchronization is disabled.
		thm := task.HookMetadataAccessor(tsk)
		s.moduleManager.GlobalSynchronizationState().QueuedForBinding(thm)
	}

	s.logTaskAdd("append", parallelSyncTasksToWait...)

	for _, tsk := range parallelSyncTasks {
		if err := s.queueService.AddLastTaskToQueue(tsk.GetQueueName(), tsk); err != nil {
			s.logger.Error("Queue is not created while run GlobalHookEnableKubernetesBindings parallel sync task!",
				slog.String("queue", tsk.GetQueueName()))
		}
	}

	s.logTaskAdd("append", parallelSyncTasks...)

	// Note: No need to add "main" Synchronization tasks to the GlobalSynchronizationState.
	res.AddHeadTasks(mainSyncTasks...)
	s.logTaskAdd("head", mainSyncTasks...)

	res.Status = queue.Success

	return res
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
