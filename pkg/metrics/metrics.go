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

	// Metric group names for grouped metrics
	ModuleInfoMetricGroup        = "mm_module_info"
	ModuleMaintenanceMetricGroup = "mm_module_maintenance"
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
	_, err = metricStorage.RegisterCounter(ConfigValuesErrorsTotal, []string{}, options.WithHelp("Total number of configuration values errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConfigValuesErrorsTotal, err)
	}

	// modules
	_, err = metricStorage.RegisterCounter(ModulesDiscoverErrorsTotal, []string{}, options.WithHelp("Total number of module discovery errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModulesDiscoverErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(ModuleDeleteErrorsTotal, []string{"module"}, options.WithHelp("Total number of module deletion errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleDeleteErrorsTotal, err)
	}

	// module
	_, err = metricStorage.RegisterHistogram(
		ModuleRunSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s,
		options.WithHelp("Time taken for module execution in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleRunSeconds, err)
	}

	_, err = metricStorage.RegisterCounter(ModuleRunErrorsTotal, []string{"module"}, options.WithHelp("Total number of module run errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleRunErrorsTotal, err)
	}

	moduleHookLabels := []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, err = metricStorage.RegisterHistogram(
		ModuleHookRunSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Time taken for module hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		ModuleHookRunUserCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("User CPU time used by module hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunUserCPUSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		ModuleHookRunSysCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("System CPU time used by module hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunSysCPUSeconds, err)
	}

	_, err = metricStorage.RegisterGauge(ModuleHookRunMaxRSSBytes, moduleHookLabels, options.WithHelp("Maximum resident set size in bytes used by module hook execution"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunMaxRSSBytes, err)
	}

	_, err = metricStorage.RegisterCounter(ModuleHookAllowedErrorsTotal, moduleHookLabels, options.WithHelp("Total number of allowed module hook errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookAllowedErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(ModuleHookErrorsTotal, moduleHookLabels, options.WithHelp("Total number of module hook errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(ModuleHookSuccessTotal, moduleHookLabels, options.WithHelp("Total number of successful module hook executions"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookSuccessTotal, err)
	}

	// global hook running
	globalHookLabels := []string{
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, err = metricStorage.RegisterHistogram(
		GlobalHookRunSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Time taken for global hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		GlobalHookRunUserCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("User CPU time used by global hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunUserCPUSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		GlobalHookRunSysCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("System CPU time used by global hook execution in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunSysCPUSeconds, err)
	}

	_, err = metricStorage.RegisterGauge(GlobalHookRunMaxRSSBytes, globalHookLabels, options.WithHelp("Maximum resident set size in bytes used by global hook execution"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunMaxRSSBytes, err)
	}

	_, err = metricStorage.RegisterCounter(GlobalHookAllowedErrorsTotal, globalHookLabels, options.WithHelp("Total number of allowed global hook errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookAllowedErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(GlobalHookErrorsTotal, globalHookLabels, options.WithHelp("Total number of global hook errors"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(GlobalHookSuccessTotal, globalHookLabels, options.WithHelp("Total number of successful global hook executions"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookSuccessTotal, err)
	}

	// converge duration
	_, err = metricStorage.RegisterCounter(ConvergenceSeconds, []string{pkg.MetricKeyActivation}, options.WithHelp("Total time spent in convergence operations in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConvergenceSeconds, err)
	}

	_, err = metricStorage.RegisterCounter(ConvergenceTotal, []string{pkg.MetricKeyActivation}, options.WithHelp("Total number of convergence operations completed"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConvergenceTotal, err)
	}

	// helm operations
	_, err = metricStorage.RegisterHistogram(
		ModuleHelmSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s,
		options.WithHelp("Time taken for Helm operations on modules in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHelmSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		HelmOperationSeconds,
		[]string{
			"module",
			pkg.MetricKeyActivation,
			"operation",
		},
		buckets_1msTo10s,
		options.WithHelp("Time taken for specific Helm operations in seconds"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", HelmOperationSeconds, err)
	}

	// task age
	// hook_run task waiting time
	_, err = metricStorage.RegisterCounter(
		TaskWaitInQueueSecondsTotal,
		[]string{
			"module",
			pkg.MetricKeyHook,
			pkg.MetricKeyBinding,
			"queue",
		},
		options.WithHelp("Total time in seconds that tasks have waited in queue before execution"))
	if err != nil {
		return fmt.Errorf("can not register %s: %w", TaskWaitInQueueSecondsTotal, err)
	}

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
