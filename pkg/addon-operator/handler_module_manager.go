package addon_operator

import (
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/gofrs/uuid/v5"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/metrics"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

func (op *AddonOperator) StartModuleManagerEventHandler() {
	go func() {
		logEntry := op.Logger.With("operator.component", "handleManagerEvents")
		for {
			select {
			case schedulerEvent := <-op.ModuleManager.SchedulerEventCh():
				switch event := schedulerEvent.EncapsulatedEvent.(type) {
				// dynamically_enabled_extender
				case dynamic_extender.DynamicExtenderEvent:
					logLabels := map[string]string{
						"event.id":     uuid.Must(uuid.NewV4()).String(),
						"type":         "ModuleScheduler event",
						"event_source": "DymicallyEnabledExtenderChanged",
					}
					eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)
					// if global hooks haven't been run yet, script enabled extender fails due to missing global values
					if op.globalHooksNotExecutedYet() {
						eventLogEntry.Info("Global hook dynamic modification detected, ignore until starting first converge")
						break
					}

					graphStateChanged := op.ModuleManager.RecalculateGraph(logLabels)

					if graphStateChanged {
						convergeTask := converge.NewConvergeModulesTask(
							"ReloadAll-After-GlobalHookDynamicUpdate",
							converge.ReloadAllModules,
							logLabels,
						)
						// if converge has already begun - restart it immediately
						if op.engine.TaskQueues.GetMain().Length() > 0 && RemoveCurrentConvergeTasks(op.getConvergeQueues(), logLabels, op.Logger) && op.ConvergeState.GetPhase() != converge.StandBy {
							logEntry.Info("ConvergeModules: global hook dynamic modification detected, restart current converge process",
								slog.String("phase", string(op.ConvergeState.GetPhase())))
							op.engine.TaskQueues.GetMain().AddFirst(convergeTask)
							op.logTaskAdd(eventLogEntry, "DynamicExtender is updated, put first", convergeTask)
						} else {
							// if convege hasn't started - make way for global hooks and etc
							logEntry.Info("ConvergeModules:  global hook dynamic modification detected, rerun all modules required")
							op.engine.TaskQueues.GetMain().AddLast(convergeTask)
						}
						// ConvergeModules may be in progress, Reset converge state.
						op.ConvergeState.SetPhase(converge.StandBy)
					}

				// kube_config_extender
				case config.KubeConfigEvent:
					logLabels := map[string]string{
						"event.id":     uuid.Must(uuid.NewV4()).String(),
						"type":         "ModuleScheduler event",
						"event_source": "KubeConfigExtenderChanged",
					}
					eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)
					switch event.Type {
					case config.KubeConfigInvalid:
						op.ModuleManager.SetKubeConfigValid(false)
						eventLogEntry.Info("KubeConfig become invalid")

					case config.KubeConfigChanged:
						eventLogEntry.Debug("ModuleManagerEventHandler-KubeConfigChanged",
							slog.Bool("globalSectionChanged", event.GlobalSectionChanged),
							slog.Any("moduleValuesChanged", event.ModuleValuesChanged),
							slog.Any("moduleEnabledStateChanged", event.ModuleEnabledStateChanged),
							slog.Any("ModuleMaintenanceChanged", event.ModuleMaintenanceChanged))
						if !op.ModuleManager.GetKubeConfigValid() {
							eventLogEntry.Info("KubeConfig become valid")
						}
						// Config is valid now, add task to update ModuleManager state.
						op.ModuleManager.SetKubeConfigValid(true)

						var (
							kubeConfigTask sh_task.Task
							convergeTask   sh_task.Task
						)

						if event.GlobalSectionChanged || len(event.ModuleValuesChanged)+len(event.ModuleMaintenanceChanged) > 0 {
							kubeConfigTask = converge.NewApplyKubeConfigValuesTask(
								"Apply-Kube-Config-Values-Changes",
								logLabels,
								event.GlobalSectionChanged,
							)
						}

						for module, state := range event.ModuleMaintenanceChanged {
							op.ModuleManager.SetModuleMaintenanceState(module, state)
						}

						// if global hooks haven't been run yet, script enabled extender fails due to missing global values,
						// but it's ok to apply new kube config
						if op.globalHooksNotExecutedYet() {
							if kubeConfigTask != nil {
								op.engine.TaskQueues.GetMain().AddFirst(kubeConfigTask)
								// Cancel delay in case the head task is stuck in the error loop.
								op.engine.TaskQueues.GetMain().CancelTaskDelay()
								op.logTaskAdd(eventLogEntry, "KubeConfigExtender is updated, put first", kubeConfigTask)
							}
							eventLogEntry.Info("Kube config modification detected, ignore until starting first converge")
							break
						}

						graphStateChanged := op.ModuleManager.RecalculateGraph(logLabels)

						if event.GlobalSectionChanged || graphStateChanged {
							// prepare convergeModules task
							convergeTask = converge.NewConvergeModulesTask(
								"ReloadAll-After-KubeConfigChange",
								converge.ReloadAllModules,
								logLabels,
							)
							// if main queue isn't empty and there was another convergeModules task:
							if op.engine.TaskQueues.GetMain().Length() > 0 && RemoveCurrentConvergeTasks(op.getConvergeQueues(), logLabels, op.Logger) {
								logEntry.Info("ConvergeModules: kube config modification detected,  restart current converge process",
									slog.String("phase", string(op.ConvergeState.GetPhase())))
								// put ApplyKubeConfig->NewConvergeModulesTask sequence in the beginning of the main queue
								if kubeConfigTask != nil {
									op.engine.TaskQueues.GetMain().AddFirst(kubeConfigTask)
									op.engine.TaskQueues.GetMain().AddAfter(kubeConfigTask.GetId(), convergeTask)
									op.logTaskAdd(eventLogEntry, "KubeConfig is changed, put after AppplyNewKubeConfig", convergeTask)
									// otherwise, put just NewConvergeModulesTask
								} else {
									op.engine.TaskQueues.GetMain().AddFirst(convergeTask)
									op.logTaskAdd(eventLogEntry, "KubeConfig is changed, put first", convergeTask)
								}
								// if main queue is empty - put NewConvergeModulesTask in the end of the queue
							} else {
								// not forget to cram in ApplyKubeConfig task
								if kubeConfigTask != nil {
									op.engine.TaskQueues.GetMain().AddFirst(kubeConfigTask)
								}
								logEntry.Info("ConvergeModules: kube config modification detected, rerun all modules required")
								op.engine.TaskQueues.GetMain().AddLast(convergeTask)
							}
							// ConvergeModules may be in progress, Reset converge state.
							op.ConvergeState.SetPhase(converge.StandBy)
						} else {
							modulesToRerun := make([]string, 0, len(event.ModuleValuesChanged)+len(event.ModuleMaintenanceChanged))
							for _, moduleName := range event.ModuleValuesChanged {
								if op.ModuleManager.IsModuleEnabled(moduleName) {
									modulesToRerun = append(modulesToRerun, moduleName)
								}
							}

							for moduleName := range event.ModuleMaintenanceChanged {
								if !slices.Contains(modulesToRerun, moduleName) && op.ModuleManager.IsModuleEnabled(moduleName) {
									modulesToRerun = append(modulesToRerun, moduleName)
								}
							}

							// Append ModuleRun tasks if ModuleRun is not queued already.
							if kubeConfigTask != nil {
								reloadTasks := op.CreateReloadModulesTasks(modulesToRerun, kubeConfigTask.GetLogLabels(), "KubeConfig-Changed-Modules")
								op.engine.TaskQueues.GetMain().AddFirst(kubeConfigTask)
								if len(reloadTasks) > 0 {
									for i := len(reloadTasks) - 1; i >= 0; i-- {
										op.engine.TaskQueues.GetMain().AddAfter(kubeConfigTask.GetId(), reloadTasks[i])
									}
									logEntry.Info("ConvergeModules: kube config modification detected, append tasks to rerun modules",
										slog.Int("count", len(reloadTasks)),
										slog.Any("modules", modulesToRerun))
									op.logTaskAdd(logEntry, "tail", reloadTasks...)
								}
							}
						}
					}
				// TODO: some other extenders' events
				default:
				}

			case HelmReleaseStatusEvent := <-op.HelmResourcesManager.Ch():
				logLabels := map[string]string{
					"event.id": uuid.Must(uuid.NewV4()).String(),
					"module":   HelmReleaseStatusEvent.ModuleName,
				}
				eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)

				// Do not add ModuleRun task if it is already queued.
				hasTask := QueueHasPendingModuleRunTask(op.engine.TaskQueues.GetMain(), HelmReleaseStatusEvent.ModuleName)
				eventDescription := "AbsentHelmResourcesDetected"
				additionalDescription := fmt.Sprintf("%d absent module resources", len(HelmReleaseStatusEvent.Absent))
				// helm reslease in unexpected state event
				if HelmReleaseStatusEvent.UnexpectedStatus {
					op.engine.MetricStorage.CounterAdd(metrics.ModulesHelmReleaseRedeployedTotal, 1.0, map[string]string{"module": HelmReleaseStatusEvent.ModuleName})
					eventDescription = "HelmReleaseUnexpectedStatus"
					additionalDescription = "unexpected helm release status"
				} else {
					// some resources are missing and metrics are provided
					for _, manifest := range HelmReleaseStatusEvent.Absent {
						op.engine.MetricStorage.CounterAdd(metrics.ModulesAbsentResourcesTotal, 1.0, map[string]string{"module": HelmReleaseStatusEvent.ModuleName, "resource": fmt.Sprintf("%s/%s/%s", manifest.Namespace(""), manifest.Kind(), manifest.Name())})
					}
				}

				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: eventDescription,
							ModuleName:       HelmReleaseStatusEvent.ModuleName,
						})
					op.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					op.logTaskAdd(logEntry, fmt.Sprintf("detected %s, append", additionalDescription), newTask)
				} else {
					eventLogEntry.With("task.flow", "noop").Info("Detected event, ModuleRun task already queued",
						slog.String("description", additionalDescription))
				}
			}
		}
	}()
}
