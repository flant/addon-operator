package convergemodules

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	hookTypes "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules/events"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/functional"
	"github.com/flant/addon-operator/pkg/task/helpers"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	bc "github.com/flant/shell-operator/pkg/hook/binding_context"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "converge-modules"

	// repeatInterval is the interval at which the convergence status is checked
	// while waiting until the functional scheduler done
	repeatInterval = 3 * time.Second
)

// TaskDependencies defines the external dependencies required by the ConvergeModules task.
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetConvergeState() *converge.ConvergeState
	GetQueueService() *taskqueue.Service
	GetFunctionalScheduler() *functional.Scheduler
}

// RegisterTaskHandler creates a factory function that instantiates ConvergeModules tasks.
func RegisterTaskHandler(deps TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(shellTask sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			shellTask,
			deps.GetModuleManager(),
			deps.GetMetricStorage(),
			deps.GetConvergeState(),
			deps.GetFunctionalScheduler(),
			deps.GetQueueService(),
			logger.Named("converge-modules"),
		)
	}
}

// Task handles module convergence operations.
type Task struct {
	shellTask sh_task.Task

	moduleManager       *module_manager.ModuleManager
	metricStorage       metricsstorage.Storage
	convergeState       *converge.ConvergeState
	queueService        *taskqueue.Service
	functionalScheduler *functional.Scheduler

	logger *log.Logger
}

// NewTask creates a new module convergence task handler.
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	metricStorage metricsstorage.Storage,
	convergeState *converge.ConvergeState,
	functionalScheduler *functional.Scheduler,
	queueService *taskqueue.Service,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:           shellTask,
		moduleManager:       moduleManager,
		metricStorage:       metricStorage,
		convergeState:       convergeState,
		queueService:        queueService,
		functionalScheduler: functionalScheduler,
		logger:              logger,
	}
}

// Handle is a multi-phase task.
func (s *Task) Handle(ctx context.Context) sh_task.Result {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res sh_task.Result

	taskEvent, ok := s.shellTask.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent)
	if !ok {
		s.logger.Error("Possible bug! Wrong prop type in ConvergeModules: got another type instead string.",
			slog.String("type", fmt.Sprintf("%T(%#[1]v)", s.shellTask.GetProp("event"))))

		res.Status = sh_task.Fail

		return res
	}

	hm := task.HookMetadataAccessor(s.shellTask)

	var handleErr error

	if s.convergeState.GetPhase() == converge.StandBy {
		s.logger.Debug("ConvergeModules: start")

		// Deduplicate tasks: remove ConvergeModules tasks right after the current task.
		s.queueService.RemoveAdjacentConvergeModules(s.shellTask.GetQueueName(), s.shellTask.GetId())

		s.convergeState.SetPhase(converge.RunBeforeAll)
	}

	if s.convergeState.GetPhase() == converge.RunBeforeAll {
		// Put BeforeAll tasks before current task.
		tasks := s.CreateBeforeAllTasks(s.shellTask.GetLogLabels(), hm.EventDescription)
		s.convergeState.SetPhase(converge.WaitBeforeAll)

		if len(tasks) > 0 {
			res.AddHeadTasks(tasks...)
			res.Status = sh_task.Keep

			s.logTaskAdd("head", tasks...)

			return res
		}
	}

	if s.convergeState.GetPhase() == converge.WaitBeforeAll {
		s.logger.Info("ConvergeModules: beforeAll hooks done, run modules")

		var state *module_manager.ModulesState

		state, handleErr = s.moduleManager.RefreshEnabledState(s.shellTask.GetLogLabels())
		if handleErr == nil {
			// TODO disable hooks before was done in DiscoverModulesStateRefresh. Should we stick to this solution or disable events later during the handling each ModuleDelete task?
			// Disable events for disabled modules.
			for _, moduleName := range state.ModulesToDisable {
				s.moduleManager.DisableModuleHooks(moduleName)
				// op.DrainModuleQueues(moduleName)
			}
			// Set ModulesToEnable list to properly run onStartup hooks for first converge.
			if !s.IsStartupConvergeDone() {
				state.ModulesToEnable = state.AllEnabledModules
				// send ModuleEvents for each disabled module on first converge to update dsabled modules' states (for the sake of disabled by <extender_name>)
				enabledModules := make(map[string]struct{}, len(state.AllEnabledModules))
				for _, enabledModule := range state.AllEnabledModules {
					enabledModules[enabledModule] = struct{}{}
				}

				s.logger.Debug("ConvergeModules: send module disabled events")
				go func() {
					for _, moduleName := range s.moduleManager.GetModuleNames() {
						if _, enabled := enabledModules[moduleName]; !enabled {
							s.moduleManager.SendModuleEvent(events.ModuleEvent{
								ModuleName: moduleName,
								EventType:  events.ModuleDisabled,
							})
						}
					}
				}()
			}
			tasks := s.CreateConvergeModulesTasks(state, s.shellTask.GetLogLabels(), string(taskEvent))

			s.convergeState.SetPhase(converge.WaitDeleteAndRunModules)
			if len(tasks) > 0 {
				res.AddHeadTasks(tasks...)
				res.Status = sh_task.Keep
				s.logTaskAdd("head", tasks...)

				return res
			}
		}
	}

	if s.convergeState.GetPhase() == converge.WaitDeleteAndRunModules {
		// trigger functional converge
		s.functionalScheduler.Done(functional.Root)

		// wait until functional converge done
		if !s.functionalScheduler.Finished() {
			s.logger.Warn("ConvergeModules: functional scheduler not finished")
			res.Status = sh_task.Keep
			res.DelayBeforeNextTask = repeatInterval

			if s.queueService.GetQueueLength(queue.MainQueueName) > 1 {
				s.logger.Debug("ConvergeModules: main queue has pending tasks, pass them")
				res.DelayBeforeNextTask = 0
				res.AddTailTasks(s.shellTask.DeepCopyWithNewUUID())
				res.Status = sh_task.Success
				s.logTaskAdd("tail", s.shellTask)
			}

			return res
		}

		s.logger.Info("ConvergeModules: ModuleRun tasks done, execute AfterAll global hooks")
		// Put AfterAll tasks before current task.
		tasks, handleErr := s.CreateAfterAllTasks(s.shellTask.GetLogLabels(), hm.EventDescription)
		if handleErr == nil {
			s.convergeState.SetPhase(converge.WaitAfterAll)
			if len(tasks) > 0 {
				res.AddHeadTasks(tasks...)
				res.Status = sh_task.Keep
				s.logTaskAdd("head", tasks...)
				return res
			}
		}
	}

	// It is the last phase of ConvergeModules task, reset operator's Converge phase.
	if s.convergeState.GetPhase() == converge.WaitAfterAll {
		s.convergeState.SetPhase(converge.StandBy)

		s.logger.Info("ConvergeModules task done")

		res.Status = sh_task.Success

		return res
	}

	if handleErr != nil {
		res.Status = sh_task.Fail
		s.logger.Error("ConvergeModules failed, requeue task to retry after delay.",
			slog.String("phase", string(s.convergeState.GetPhase())),
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(handleErr))
		s.metricStorage.CounterAdd(metrics.ModulesDiscoverErrorsTotal, 1.0, map[string]string{})
		s.shellTask.UpdateFailureMessage(handleErr.Error())
		s.shellTask.WithQueuedAt(time.Now())
		return res
	}

	s.logger.Debug("ConvergeModules success")
	res.Status = sh_task.Success

	return res
}

// CreateBeforeAllTasks returns tasks to run BeforeAll global hooks.
func (s *Task) CreateBeforeAllTasks(logLabels map[string]string, eventDescription string) []sh_task.Task {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'beforeAll' global hooks.
	beforeAllHooks := s.moduleManager.GetGlobalHooksInOrder(hookTypes.BeforeAll)

	for _, hookName := range beforeAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook:    hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(hookTypes.BeforeAll),
		})
		// remove task.id — it is set by NewTask
		delete(hookLogLabels, "task.id")

		// bc := module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: stringmodule_manager.BeforeAll)}}
		// bc.KubernetesSnapshots := ModuleManager.GetGlobalHook(hookName).HookController.KubernetesSnapshots()

		beforeAllBc := bc.BindingContext{
			Binding: string(hookTypes.BeforeAll),
		}
		beforeAllBc.Metadata.BindingType = hookTypes.BeforeAll
		beforeAllBc.Metadata.IncludeAllSnapshots = true

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              hookTypes.BeforeAll,
				BindingContext:           []bc.BindingContext{beforeAllBc},
				ReloadAllOnValuesChanges: false,
			}).
			WithCompactionID(hookName)
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	return tasks
}

// CreateAfterAllTasks returns tasks to run AfterAll global hooks.
func (s *Task) CreateAfterAllTasks(logLabels map[string]string, eventDescription string) ([]sh_task.Task, error) {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'afterAll' global hooks.
	afterAllHooks := s.moduleManager.GetGlobalHooksInOrder(hookTypes.AfterAll)

	for i, hookName := range afterAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook:    hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(hookTypes.AfterAll),
		})
		delete(hookLogLabels, "task.id")

		afterAllBc := bc.BindingContext{
			Binding: string(hookTypes.AfterAll),
		}
		afterAllBc.Metadata.BindingType = hookTypes.AfterAll
		afterAllBc.Metadata.IncludeAllSnapshots = true

		taskMetadata := task.HookMetadata{
			EventDescription: eventDescription,
			HookName:         hookName,
			BindingType:      hookTypes.AfterAll,
			BindingContext:   []bc.BindingContext{afterAllBc},
		}
		if i == len(afterAllHooks)-1 {
			taskMetadata.LastAfterAllHook = true
			globalValues := s.moduleManager.GetGlobal().GetValues(false)
			taskMetadata.ValuesChecksum = globalValues.Checksum()
		}

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(taskMetadata).
			WithCompactionID(taskMetadata.HookName)
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	return tasks, nil
}

// CreateConvergeModulesTasks creates tasks for module lifecycle management based on the current state.
// It generates:
// - ModuleEnsureCRDs tasks for modules that need CRD installation
// - ModuleDelete tasks for modules that need to be disabled
// - ModuleRun tasks for individual modules that need to be enabled or rerun
// - ParallelModuleRun tasks for groups of modules that can be processed in parallel
func (s *Task) CreateConvergeModulesTasks(state *module_manager.ModulesState, logLabels map[string]string, eventDescription string,
) []sh_task.Task {
	modulesTasks := make([]sh_task.Task, 0, len(state.ModulesToDisable)+len(state.AllEnabledModules))
	resultingTasks := make([]sh_task.Task, 0, len(state.ModulesToDisable)+len(state.AllEnabledModules))
	queuedAt := time.Now()

	// Add ModuleDelete tasks to delete helm releases of disabled modules.
	log.Debug("The following modules are going to be disabled",
		slog.Any("modules", state.ModulesToDisable))
	for _, moduleName := range state.ModulesToDisable {
		ev := events.ModuleEvent{
			ModuleName: moduleName,
			EventType:  events.ModuleDisabled,
		}
		s.moduleManager.SendModuleEvent(ev)
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleDelete).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))

		// undone, unschedule and remove the disabled module from the functional scheduler
		s.functionalScheduler.Remove(moduleName)
	}

	// Add ModuleRun tasks to install or reload enabled modules.
	newlyEnabled := utils.ListToMapStringStruct(state.ModulesToEnable)
	log.Debug("The following modules are going to be enabled/rerun",
		slog.String("modules", fmt.Sprintf("%v", state.AllEnabledModulesByOrder)))

	var functionalModules []string
	for _, modules := range state.AllEnabledModulesByOrder {
		if len(modules) == 0 {
			continue
		}

		// skip functional modules
		if !s.moduleManager.GetCritical(modules[0]) {
			functionalModules = append(functionalModules, modules...)
			continue
		}

		newLogLabels := utils.MergeLabels(logLabels)
		delete(newLogLabels, "task.id")
		switch {
		// create parallel moduleRun task
		case len(modules) > 1:
			parallelRunMetadata := task.ParallelRunMetadata{}
			newLogLabels["modules"] = strings.Join(modules, ",")
			for _, moduleName := range modules {
				ev := events.ModuleEvent{
					ModuleName: moduleName,
					EventType:  events.ModuleEnabled,
				}
				s.moduleManager.SendModuleEvent(ev)
				doModuleStartup := false
				if _, has := newlyEnabled[moduleName]; has {
					// add EnsureCRDs task if module is about to be enabled
					if s.moduleManager.ModuleHasCRDs(moduleName) {
						resultingTasks = append(resultingTasks, sh_task.NewTask(task.ModuleEnsureCRDs).
							WithLogLabels(newLogLabels).
							WithQueueName("main").
							WithMetadata(task.HookMetadata{
								EventDescription: "EnsureCRDs",
								ModuleName:       moduleName,
								IsReloadAll:      true,
							}).WithQueuedAt(queuedAt))
					}
					doModuleStartup = true
				}
				parallelRunMetadata.SetModuleMetadata(moduleName, task.ParallelRunModuleMetadata{
					DoModuleStartup: doModuleStartup,
				})
			}
			parallelRunMetadata.Context, parallelRunMetadata.CancelF = context.WithCancel(context.Background())
			newTask := sh_task.NewTask(task.ParallelModuleRun).
				WithLogLabels(newLogLabels).
				WithQueueName("main").
				WithMetadata(task.HookMetadata{
					EventDescription:    eventDescription,
					ModuleName:          fmt.Sprintf("Parallel run for %s", strings.Join(modules, ", ")),
					IsReloadAll:         true,
					Critical:            true,
					ParallelRunMetadata: &parallelRunMetadata,
				})
			modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))

		// otherwise, create an original moduleRun task
		case len(modules) == 1:
			ev := events.ModuleEvent{
				ModuleName: modules[0],
				EventType:  events.ModuleEnabled,
			}
			s.moduleManager.SendModuleEvent(ev)
			newLogLabels["module"] = modules[0]
			doModuleStartup := false
			if _, has := newlyEnabled[modules[0]]; has {
				// add EnsureCRDs task if module is about to be enabled
				if s.moduleManager.ModuleHasCRDs(modules[0]) {
					resultingTasks = append(resultingTasks, sh_task.NewTask(task.ModuleEnsureCRDs).
						WithLogLabels(newLogLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "EnsureCRDs",
							ModuleName:       modules[0],
							IsReloadAll:      true,
						}).WithQueuedAt(queuedAt))
				}
				doModuleStartup = true
			}
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLogLabels).
				WithQueueName("main").
				WithMetadata(task.HookMetadata{
					EventDescription: eventDescription,
					ModuleName:       modules[0],
					DoModuleStartup:  doModuleStartup,
					IsReloadAll:      true,
					Critical:         true,
				})
			modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))

		default:
			log.Error("Invalid ModulesState",
				slog.String("state", fmt.Sprintf("%v", state)))
		}
	}

	deps := s.moduleManager.GetFunctionalDependencies()

	// handle functional modules
	schedulerRequests := make([]*functional.Request, len(functionalModules))
	for idx, module := range functionalModules {
		// notify about module enabling
		ev := events.ModuleEvent{
			ModuleName: module,
			EventType:  events.ModuleEnabled,
		}
		s.moduleManager.SendModuleEvent(ev)

		newLogLabels := utils.MergeLabels(logLabels)
		delete(newLogLabels, "task.id")
		newLogLabels["module"] = module

		doModuleStartup := false
		// add EnsureCRDs task if module is about to be enabled
		if _, has := newlyEnabled[module]; has {
			if s.moduleManager.ModuleHasCRDs(module) {
				resultingTasks = append(resultingTasks, sh_task.NewTask(task.ModuleEnsureCRDs).
					WithLogLabels(newLogLabels).
					WithQueueName("main").
					WithMetadata(task.HookMetadata{
						EventDescription: "EnsureCRDs",
						ModuleName:       module,
						IsReloadAll:      true,
					}).WithQueuedAt(queuedAt))
			}
			doModuleStartup = true
		}

		schedulerRequests[idx] = &functional.Request{
			Name:         module,
			Dependencies: deps[module],
			Description:  eventDescription,
			IsReloadAll:  true,
			DoStartup:    doModuleStartup,
			Labels:       newLogLabels,
		}
	}

	// schedule functional modules in parallel queues
	if len(schedulerRequests) > 0 {
		s.functionalScheduler.Add(schedulerRequests...)
	}

	// as resultingTasks contains new ensureCRDsTasks we invalidate
	// ConvregeState.CRDsEnsured if there are new ensureCRDsTasks to execute
	if s.convergeState.CRDsEnsured && len(resultingTasks) > 0 {
		log.Debug("CheckCRDsEnsured: set to false")
		s.convergeState.CRDsEnsured = false
	}

	// append modulesTasks to resultingTasks
	resultingTasks = append(resultingTasks, modulesTasks...)

	return resultingTasks
}

func (s *Task) IsStartupConvergeDone() bool {
	return s.convergeState.GetFirstRunPhase() == converge.FirstDone
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
