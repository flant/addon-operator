package parallelmodulerun

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	paralleltask "github.com/flant/addon-operator/pkg/task/parallel"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "parallel-module-run"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetQueueService() *taskqueue.Service
	GetParallelTaskChannels() *paralleltask.TaskChannels
}

// RegisterTaskHandler creates a factory function for ParallelModuleRun tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			svc.GetQueueService(),
			svc.GetParallelTaskChannels(),
			logger.Named("parallel-module-run"),
		)
	}
}

// Task handles running multiple module tasks in parallel
type Task struct {
	shellTask            sh_task.Task
	moduleManager        *module_manager.ModuleManager
	queueService         *taskqueue.Service
	parallelTaskChannels *paralleltask.TaskChannels
	logger               *log.Logger
}

// NewTask creates a new task handler for parallel module execution
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	queueService *taskqueue.Service,
	parallelTaskChannels *paralleltask.TaskChannels,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:            shellTask,
		moduleManager:        moduleManager,
		queueService:         queueService,
		parallelTaskChannels: parallelTaskChannels,
		logger:               logger,
	}
}

// Handle runs multiple ModuleRun tasks in parallel and aggregates their results.
// It creates tasks for each module in separate parallel queues, then monitors their completion
// through a communication channel.
func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res queue.TaskResult

	hm := task.HookMetadataAccessor(s.shellTask)

	if hm.ParallelRunMetadata == nil {
		s.logger.Error("Possible bug! Couldn't get task ParallelRunMetadata for a parallel task.",
			slog.String("description", hm.EventDescription))
		res.Status = queue.Fail
		return res
	}

	i := 0

	parallelChannel := paralleltask.NewTaskChannel()
	s.parallelTaskChannels.Set(s.shellTask.GetId(), parallelChannel)

	s.logger.Debug("ParallelModuleRun available parallel event channels",
		slog.String("channels", fmt.Sprintf("%v", s.parallelTaskChannels.Channels())))

	for moduleName, moduleMetadata := range hm.ParallelRunMetadata.GetModulesMetadata() {
		queueName := fmt.Sprintf(app.ParallelQueueNamePattern, i%(app.NumberOfParallelQueues-1))
		newLogLabels := utils.MergeLabels(s.shellTask.GetLogLabels())
		newLogLabels[pkg.LogKeyModule] = moduleName

		delete(newLogLabels, pkg.LogKeyTaskID)

		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName(queueName).
			WithMetadata(task.HookMetadata{
				EventDescription: hm.EventDescription,
				ModuleName:       moduleName,
				DoModuleStartup:  moduleMetadata.DoModuleStartup,
				IsReloadAll:      hm.IsReloadAll,
				Critical:         hm.Critical,
				ParallelRunMetadata: &task.ParallelRunMetadata{
					ChannelId: s.shellTask.GetId(),
				},
			})

		_ = s.queueService.AddLastTaskToQueue(queueName, newTask)

		i++
	}

	// map to hold modules' errors
	tasksErrors := make(map[string]string)
L:
	for {
		select {
		case parallelEvent := <-parallelChannel:
			s.logger.Debug("ParallelModuleRun event received",
				slog.String("event", fmt.Sprintf("%v", parallelEvent)))

			if len(parallelEvent.ErrorMessage()) != 0 {
				if tasksErrors[parallelEvent.ModuleName()] != parallelEvent.ErrorMessage() {
					tasksErrors[parallelEvent.ModuleName()] = parallelEvent.ErrorMessage()
					s.shellTask.UpdateFailureMessage(formatErrorSummary(tasksErrors))
				}

				s.shellTask.IncrementFailureCount()

				continue L
			}
			if parallelEvent.Succeeded() {
				hm.ParallelRunMetadata.DeleteModuleMetadata(parallelEvent.ModuleName())

				// all ModuleRun tasks were executed successfully
				if len(hm.ParallelRunMetadata.GetModulesMetadata()) == 0 {
					break L
				}

				delete(tasksErrors, parallelEvent.ModuleName())

				s.shellTask.UpdateFailureMessage(formatErrorSummary(tasksErrors))

				newMeta := task.HookMetadata{
					EventDescription:    hm.EventDescription,
					ModuleName:          fmt.Sprintf("Parallel run for %s", strings.Join(hm.ParallelRunMetadata.ListModules(), ", ")),
					IsReloadAll:         hm.IsReloadAll,
					ParallelRunMetadata: hm.ParallelRunMetadata,
				}
				s.shellTask.UpdateMetadata(newMeta)
			}

		case <-hm.ParallelRunMetadata.Context.Done():
			s.logger.Debug("ParallelModuleRun context canceled")

			// remove channel from map so that ModuleRun handlers couldn't send into it
			s.parallelTaskChannels.Delete(s.shellTask.GetId())

			t := time.NewTimer(time.Second * 3)
			for {
				select {
				// wait for several seconds if any ModuleRun task wants to send an event
				case <-t.C:
					s.logger.Debug("ParallelModuleRun task aborted")

					res.Status = queue.Success

					return res

				// drain channel to unblock handlers if any
				case <-parallelChannel:
				}
			}
		}
	}

	s.parallelTaskChannels.Delete(s.shellTask.GetId())

	res.Status = queue.Success

	return res
}

// CreateAndStartQueuesForModuleHooks creates queues for registered module hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (s *Task) CreateAndStartQueuesForModuleHooks(moduleName string) {
	m := s.moduleManager.GetModule(moduleName)
	if m == nil {
		return
	}

	scheduleHooks := m.GetHooks(htypes.Schedule)
	for _, hook := range scheduleHooks {
		for _, hookBinding := range hook.GetHookConfig().Schedules {
			if !s.queueService.IsQueueExists(hookBinding.Queue) {
				s.queueService.CreateAndStartQueueWithCallback(hookBinding.Queue, taskqueue.UniversalCompactionCallback(s.moduleManager, s.logger))

				log.Debug("Queue started for module 'schedule'",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hook.GetName()))
			}
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			if !s.queueService.IsQueueExists(hookBinding.Queue) {
				s.queueService.CreateAndStartQueueWithCallback(hookBinding.Queue, taskqueue.UniversalCompactionCallback(s.moduleManager, s.logger))

				log.Debug("Queue started for module 'kubernetes'",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hook.GetName()))
			}
		}
	}
}

// fomartErrorSumamry constructs a string of errors from the map
func formatErrorSummary(errors map[string]string) string {
	errSummary := "\n\tErrors:\n"
	for moduleName, moduleErr := range errors {
		errSummary += fmt.Sprintf("\t- %s: %s", moduleName, moduleErr)
	}

	return errSummary
}
