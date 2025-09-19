package metrics

import (
	"fmt"
	"time"

	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/deckhouse/deckhouse/pkg/metrics-storage/options"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	BindingCount                      = "{PREFIX}binding_count"
	ConfigValuesErrorsTotal           = "{PREFIX}config_values_errors_total"
	ModulesDiscoverErrorsTotal        = "{PREFIX}modules_discover_errors_total"
	ModuleDeleteErrorsTotal           = "{PREFIX}module_delete_errors_total"
	ModuleRunSeconds                  = "{PREFIX}module_run_seconds"
	ModuleRunErrorsTotal              = "{PREFIX}module_run_errors_total"
	ModuleHookRunSeconds              = "{PREFIX}module_hook_run_seconds"
	ModuleHookRunUserCPUSeconds       = "{PREFIX}module_hook_run_user_cpu_seconds"
	ModuleHookRunSysCPUSeconds        = "{PREFIX}module_hook_run_sys_cpu_seconds"
	ModuleHookRunMaxRSSBytes          = "{PREFIX}module_hook_run_max_rss_bytes"
	ModuleHookAllowedErrorsTotal      = "{PREFIX}module_hook_allowed_errors_total"
	ModuleHookErrorsTotal             = "{PREFIX}module_hook_errors_total"
	ModuleHookSuccessTotal            = "{PREFIX}module_hook_success_total"
	GlobalHookRunSeconds              = "{PREFIX}global_hook_run_seconds"
	GlobalHookRunUserCPUSeconds       = "{PREFIX}global_hook_run_user_cpu_seconds"
	GlobalHookRunSysCPUSeconds        = "{PREFIX}global_hook_run_sys_cpu_seconds"
	GlobalHookRunMaxRSSBytes          = "{PREFIX}global_hook_run_max_rss_bytes"
	GlobalHookAllowedErrorsTotal      = "{PREFIX}global_hook_allowed_errors_total"
	GlobalHookErrorsTotal             = "{PREFIX}global_hook_errors_total"
	GlobalHookSuccessTotal            = "{PREFIX}global_hook_success_total"
	ConvergenceSeconds                = "{PREFIX}convergence_seconds"
	ConvergenceTotal                  = "{PREFIX}convergence_total"
	ModuleHelmSeconds                 = "{PREFIX}module_helm_seconds"
	HelmOperationSeconds              = "{PREFIX}helm_operation_seconds"
	TaskWaitInQueueSecondsTotal       = "{PREFIX}task_wait_in_queue_seconds_total"
	LiveTicks                         = "{PREFIX}live_ticks"
	TasksQueueLength                  = "{PREFIX}tasks_queue_length"
	ModuleManagerModuleInfo           = "{PREFIX}mm_module_info"
	ModuleManagerModuleMaintenance    = "{PREFIX}mm_module_maintenance"
	ModulesHelmReleaseRedeployedTotal = "{PREFIX}modules_helm_release_redeployed_total"
	ModulesAbsentResourcesTotal       = "{PREFIX}modules_absent_resources_total"
)

var buckets_1msTo10s = []float64{
	0.0,
	0.001, 0.002, 0.005, // 1,2,5 milliseconds
	0.01, 0.02, 0.05, // 10,20,50 milliseconds
	0.1, 0.2, 0.5, // 100,200,500 milliseconds
	1, 2, 5, // 1,2,5 seconds
	10, // 10 seconds
}

// RegisterHookMetrics register metrics specified for addon-operator
func RegisterHookMetrics(metricStorage metricsstorage.Storage) error {
	// configuration metrics
	_, err := metricStorage.RegisterGauge(
		BindingCount,
		[]string{
			"module",
			pkg.MetricKeyHook,
		}, options.WithHelp("Number of bindings for the hook in the module"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", BindingCount, err)
	}

	// ConfigMap validation errors
	_, _ = metricStorage.RegisterCounter(ConfigValuesErrorsTotal, []string{})

	// modules
	_, _ = metricStorage.RegisterCounter(ModulesDiscoverErrorsTotal, []string{})
	_, _ = metricStorage.RegisterCounter(ModuleDeleteErrorsTotal, []string{"module"})

	// module
	_, _ = metricStorage.RegisterHistogram(
		ModuleRunSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s,
	)
	_, _ = metricStorage.RegisterCounter(ModuleRunErrorsTotal, []string{"module"})

	moduleHookLabels := []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, _ = metricStorage.RegisterHistogram(
		ModuleHookRunSeconds,
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		ModuleHookRunUserCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		ModuleHookRunSysCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterGauge(ModuleHookRunMaxRSSBytes, moduleHookLabels)
	_, _ = metricStorage.RegisterCounter(ModuleHookAllowedErrorsTotal, moduleHookLabels)
	_, _ = metricStorage.RegisterCounter(ModuleHookErrorsTotal, moduleHookLabels)
	_, _ = metricStorage.RegisterCounter(ModuleHookSuccessTotal, moduleHookLabels)

	// global hook running
	globalHookLabels := []string{
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, _ = metricStorage.RegisterHistogram(
		GlobalHookRunSeconds,
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		GlobalHookRunUserCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		GlobalHookRunSysCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterGauge(GlobalHookRunMaxRSSBytes, globalHookLabels)
	_, _ = metricStorage.RegisterCounter(GlobalHookAllowedErrorsTotal, globalHookLabels)
	_, _ = metricStorage.RegisterCounter(GlobalHookErrorsTotal, globalHookLabels)
	_, _ = metricStorage.RegisterCounter(GlobalHookSuccessTotal, globalHookLabels)

	// converge duration
	_, _ = metricStorage.RegisterCounter(ConvergenceSeconds, []string{pkg.MetricKeyActivation})
	_, _ = metricStorage.RegisterCounter(ConvergenceTotal, []string{pkg.MetricKeyActivation})

	// helm operations
	_, _ = metricStorage.RegisterHistogram(
		ModuleHelmSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		HelmOperationSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
			"operation",
		},
		buckets_1msTo10s)

	// task age
	// hook_run task waiting time
	_, _ = metricStorage.RegisterCounter(
		TaskWaitInQueueSecondsTotal,
		[]string{
			"module",
			pkg.MetricKeyHook,
			pkg.MetricKeyBinding,
			"queue",
		})

	return nil
}

// StartLiveTicksUpdater starts a goroutine that periodically updates
// the live_ticks metric every 10 seconds.
// This metric can be used to verify that addon-operator is alive and functioning.
func StartLiveTicksUpdater(metricStorage metricsstorage.Storage) {
	// Addon-operator live ticks.
	go func() {
		for {
			metricStorage.CounterAdd(LiveTicks, 1.0, map[string]string{})

			time.Sleep(10 * time.Second)
		}
	}()
}

// StartTasksQueueLengthUpdater starts a goroutine that periodically updates
// the tasks_queue_length metric every 5 seconds.
// This metric shows the number of pending tasks in each queue, which can be useful
// for monitoring system load and potential backlog issues.
func StartTasksQueueLengthUpdater(metricStorage metricsstorage.Storage, tqs *queue.TaskQueueSet) {
	go func() {
		for {
			// Gather task queues lengths.
			tqs.Iterate(func(queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				metricStorage.GaugeSet(TasksQueueLength, queueLen, map[string]string{"queue": queue.Name})
			})

			time.Sleep(5 * time.Second)
		}
	}()
}
