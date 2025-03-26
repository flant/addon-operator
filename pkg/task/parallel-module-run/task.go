package parallelmodulerun

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/trace"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	paralleltask "github.com/flant/addon-operator/pkg/task/parallel"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskConfig interface {
	GetEngine() *shell_operator.ShellOperator
	GetHelm() *helm.ClientFactory
	GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metric.Storage
	GetQueueService() *taskqueue.Service
	GetParallelTaskChannels() *paralleltask.TaskChannels
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask:         t,
			IsOperatorStartup: helpers.IsOperatorStartupTask(t),

			Engine:               svc.GetEngine(),
			Helm:                 svc.GetHelm(),
			HelmResourcesManager: svc.GetHelmResourcesManager(),
			ModuleManager:        svc.GetModuleManager(),
			MetricStorage:        svc.GetMetricStorage(),
			QueueService:         svc.GetQueueService(),
		}

		return newParallelModuleRun(cfg, logger.Named("parallel-module-run"))
	}
}

type taskConfig struct {
	ShellTask         sh_task.Task
	IsOperatorStartup bool

	Engine               *shell_operator.ShellOperator
	ParallelTaskChannels *paralleltask.TaskChannels
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	ModuleManager        *module_manager.ModuleManager
	MetricStorage        metric.Storage
	QueueService         *taskqueue.Service
}

type Task struct {
	shellTask            sh_task.Task
	isOperatorStartup    bool
	engine               *shell_operator.ShellOperator
	parallelTaskChannels *paralleltask.TaskChannels
	helm                 *helm.ClientFactory
	// helmResourcesManager monitors absent resources created for modules.
	helmResourcesManager helm_resources_manager.HelmResourcesManager
	moduleManager        *module_manager.ModuleManager
	metricStorage        metric.Storage

	queueService *taskqueue.Service

	logger *log.Logger
}

// newParallelModuleRun creates a new task handler service
func newParallelModuleRun(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		isOperatorStartup: cfg.IsOperatorStartup,

		engine:               cfg.Engine,
		parallelTaskChannels: cfg.ParallelTaskChannels,
		helm:                 cfg.Helm,
		helmResourcesManager: cfg.HelmResourcesManager,
		moduleManager:        cfg.ModuleManager,
		metricStorage:        cfg.MetricStorage,
		queueService:         cfg.QueueService,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ParallelModuleRun").End()

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
				ParallelRunMetadata: &task.ParallelRunMetadata{
					ChannelId: s.shellTask.GetId(),
				},
			})
		s.engine.TaskQueues.GetByName(queueName).AddLast(newTask)
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
				s.queueService.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'schedule'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			if !s.queueService.IsQueueExists(hookBinding.Queue) {
				s.queueService.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'kubernetes'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
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
