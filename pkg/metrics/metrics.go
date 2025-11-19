// Package metrics provides centralized metric names and registration functions for addon-operator.
// All metric names use variables to ensure consistency and prevent typos.
// The {PREFIX} placeholder is replaced by the metrics storage with the appropriate prefix.
package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/deckhouse/deckhouse/pkg/metrics-storage/options"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// Metric name variables organized by functional area.
// Each variable represents a unique metric name used throughout addon-operator.
// These variables are initialized with prefix replacement at startup.
var (
	// ============================================================================
	// Configuration Metrics
	// ============================================================================
	// BindingCount tracks the number of bindings per module and hook
	BindingCount = "{PREFIX}binding_count"
	// ConfigValuesErrorsTotal counts ConfigMap validation errors
	ConfigValuesErrorsTotal = "{PREFIX}config_values_errors_total"

	// ============================================================================
	// Module Metrics
	// ============================================================================
	// ModulesDiscoverErrorsTotal counts errors during module discovery
	ModulesDiscoverErrorsTotal = "{PREFIX}modules_discover_errors_total"
	// ModuleDeleteErrorsTotal counts errors during module deletion
	ModuleDeleteErrorsTotal = "{PREFIX}module_delete_errors_total"
	// ModuleRunSeconds measures module execution time
	ModuleRunSeconds = "{PREFIX}module_run_seconds"
	// ModuleRunErrorsTotal counts module execution errors
	ModuleRunErrorsTotal = "{PREFIX}module_run_errors_total"

	// ============================================================================
	// Module Hook Metrics
	// ============================================================================
	// ModuleHookRunSeconds measures module hook execution time
	ModuleHookRunSeconds = "{PREFIX}module_hook_run_seconds"
	// ModuleHookRunUserCPUSeconds measures module hook user CPU usage
	ModuleHookRunUserCPUSeconds = "{PREFIX}module_hook_run_user_cpu_seconds"
	// ModuleHookRunSysCPUSeconds measures module hook system CPU usage
	ModuleHookRunSysCPUSeconds = "{PREFIX}module_hook_run_sys_cpu_seconds"
	// ModuleHookRunMaxRSSBytes tracks maximum resident set size for module hooks
	ModuleHookRunMaxRSSBytes = "{PREFIX}module_hook_run_max_rss_bytes"
	// ModuleHookAllowedErrorsTotal counts allowed module hook errors
	ModuleHookAllowedErrorsTotal = "{PREFIX}module_hook_allowed_errors_total"
	// ModuleHookErrorsTotal counts module hook execution errors
	ModuleHookErrorsTotal = "{PREFIX}module_hook_errors_total"
	// ModuleHookSuccessTotal counts successful module hook executions
	ModuleHookSuccessTotal = "{PREFIX}module_hook_success_total"

	// ============================================================================
	// Global Hook Metrics
	// ============================================================================
	// GlobalHookRunSeconds measures global hook execution time
	GlobalHookRunSeconds = "{PREFIX}global_hook_run_seconds"
	// GlobalHookRunUserCPUSeconds measures global hook user CPU usage
	GlobalHookRunUserCPUSeconds = "{PREFIX}global_hook_run_user_cpu_seconds"
	// GlobalHookRunSysCPUSeconds measures global hook system CPU usage
	GlobalHookRunSysCPUSeconds = "{PREFIX}global_hook_run_sys_cpu_seconds"
	// GlobalHookRunMaxRSSBytes tracks maximum resident set size for global hooks
	GlobalHookRunMaxRSSBytes = "{PREFIX}global_hook_run_max_rss_bytes"
	// GlobalHookAllowedErrorsTotal counts allowed global hook errors
	GlobalHookAllowedErrorsTotal = "{PREFIX}global_hook_allowed_errors_total"
	// GlobalHookErrorsTotal counts global hook execution errors
	GlobalHookErrorsTotal = "{PREFIX}global_hook_errors_total"
	// GlobalHookSuccessTotal counts successful global hook executions
	GlobalHookSuccessTotal = "{PREFIX}global_hook_success_total"

	// ============================================================================
	// Convergence Metrics
	// ============================================================================
	// ConvergenceSeconds measures convergence duration
	ConvergenceSeconds = "{PREFIX}convergence_seconds"
	// ConvergenceTotal counts convergence executions
	ConvergenceTotal = "{PREFIX}convergence_total"

	// ============================================================================
	// Helm Operations Metrics
	// ============================================================================
	// ModuleHelmSeconds measures Helm operation time for modules
	ModuleHelmSeconds = "{PREFIX}module_helm_seconds"
	// HelmOperationSeconds measures specific Helm operation durations
	HelmOperationSeconds = "{PREFIX}helm_operation_seconds"

	// ============================================================================
	// Task Queue Metrics
	// ============================================================================
	// TaskWaitInQueueSecondsTotal measures time tasks wait in queue
	TaskWaitInQueueSecondsTotal = "{PREFIX}task_wait_in_queue_seconds_total"
	// TasksQueueLength shows current length of task queues
	TasksQueueLength = "{PREFIX}tasks_queue_length"

	// ============================================================================
	// Live Ticks Metrics
	// ============================================================================
	// LiveTicks is a counter that increases every 10 seconds to indicate addon-operator is alive
	LiveTicks = "{PREFIX}live_ticks"
)

// Standard histogram buckets for timing metrics (1ms to 10s)
var buckets_1msTo10s = []float64{
	0.0,
	0.001, 0.002, 0.005, // 1,2,5 milliseconds
	0.01, 0.02, 0.05, // 10,20,50 milliseconds
	0.1, 0.2, 0.5, // 100,200,500 milliseconds
	1, 2, 5, // 1,2,5 seconds
	10, // 10 seconds
}

// ReplacePrefix replaces the {PREFIX} placeholder in a metric name with the provided prefix.
// This function is useful for testing or when you need to manually construct metric names
// with a specific prefix instead of relying on the metrics storage's automatic replacement.
func ReplacePrefix(metricName, prefix string) string {
	return strings.ReplaceAll(metricName, "{PREFIX}", prefix)
}

// InitMetrics initializes all metric name variables by replacing {PREFIX} placeholders
// with the provided prefix. This function should be called once at startup before
// registering any metrics.
func InitMetrics(prefix string) {
	// ============================================================================
	// Configuration Metrics
	// ============================================================================
	BindingCount = ReplacePrefix(BindingCount, prefix)
	ConfigValuesErrorsTotal = ReplacePrefix(ConfigValuesErrorsTotal, prefix)

	// ============================================================================
	// Module Metrics
	// ============================================================================
	ModulesDiscoverErrorsTotal = ReplacePrefix(ModulesDiscoverErrorsTotal, prefix)
	ModuleDeleteErrorsTotal = ReplacePrefix(ModuleDeleteErrorsTotal, prefix)
	ModuleRunSeconds = ReplacePrefix(ModuleRunSeconds, prefix)
	ModuleRunErrorsTotal = ReplacePrefix(ModuleRunErrorsTotal, prefix)

	// ============================================================================
	// Module Hook Metrics
	// ============================================================================
	ModuleHookRunSeconds = ReplacePrefix(ModuleHookRunSeconds, prefix)
	ModuleHookRunUserCPUSeconds = ReplacePrefix(ModuleHookRunUserCPUSeconds, prefix)
	ModuleHookRunSysCPUSeconds = ReplacePrefix(ModuleHookRunSysCPUSeconds, prefix)
	ModuleHookRunMaxRSSBytes = ReplacePrefix(ModuleHookRunMaxRSSBytes, prefix)
	ModuleHookAllowedErrorsTotal = ReplacePrefix(ModuleHookAllowedErrorsTotal, prefix)
	ModuleHookErrorsTotal = ReplacePrefix(ModuleHookErrorsTotal, prefix)
	ModuleHookSuccessTotal = ReplacePrefix(ModuleHookSuccessTotal, prefix)

	// ============================================================================
	// Global Hook Metrics
	// ============================================================================
	GlobalHookRunSeconds = ReplacePrefix(GlobalHookRunSeconds, prefix)
	GlobalHookRunUserCPUSeconds = ReplacePrefix(GlobalHookRunUserCPUSeconds, prefix)
	GlobalHookRunSysCPUSeconds = ReplacePrefix(GlobalHookRunSysCPUSeconds, prefix)
	GlobalHookRunMaxRSSBytes = ReplacePrefix(GlobalHookRunMaxRSSBytes, prefix)
	GlobalHookAllowedErrorsTotal = ReplacePrefix(GlobalHookAllowedErrorsTotal, prefix)
	GlobalHookErrorsTotal = ReplacePrefix(GlobalHookErrorsTotal, prefix)
	GlobalHookSuccessTotal = ReplacePrefix(GlobalHookSuccessTotal, prefix)

	// ============================================================================
	// Convergence Metrics
	// ============================================================================
	ConvergenceSeconds = ReplacePrefix(ConvergenceSeconds, prefix)
	ConvergenceTotal = ReplacePrefix(ConvergenceTotal, prefix)

	// ============================================================================
	// Helm Operations Metrics
	// ============================================================================
	ModuleHelmSeconds = ReplacePrefix(ModuleHelmSeconds, prefix)
	HelmOperationSeconds = ReplacePrefix(HelmOperationSeconds, prefix)

	// ============================================================================
	// Task Queue Metrics
	// ============================================================================
	TaskWaitInQueueSecondsTotal = ReplacePrefix(TaskWaitInQueueSecondsTotal, prefix)
	TasksQueueLength = ReplacePrefix(TasksQueueLength, prefix)

	// ============================================================================
	// Live Ticks Metrics
	// ============================================================================
	LiveTicks = ReplacePrefix(LiveTicks, prefix)
}

// ============================================================================
// Registration Functions
// ============================================================================

// registerHookMetrics registers all addon-operator specific metrics with the provided storage.
// This includes configuration, module, hook, convergence, Helm, and task queue metrics.
// Returns an error if any metric registration fails.
func RegisterHookMetrics(metricStorage metricsstorage.Storage) error {
	// Register configuration metrics
	if err := registerConfigurationMetrics(metricStorage); err != nil {
		return fmt.Errorf("register configuration metrics: %w", err)
	}

	// Register module metrics
	if err := registerModuleMetrics(metricStorage); err != nil {
		return fmt.Errorf("register module metrics: %w", err)
	}

	// Register module hook metrics
	if err := registerModuleHookMetrics(metricStorage); err != nil {
		return fmt.Errorf("register module hook metrics: %w", err)
	}

	// Register global hook metrics
	if err := registerGlobalHookMetrics(metricStorage); err != nil {
		return fmt.Errorf("register global hook metrics: %w", err)
	}

	// Register convergence metrics
	if err := registerConvergenceMetrics(metricStorage); err != nil {
		return fmt.Errorf("register convergence metrics: %w", err)
	}

	// Register Helm metrics
	if err := registerHelmMetrics(metricStorage); err != nil {
		return fmt.Errorf("register helm metrics: %w", err)
	}

	// Register task queue metrics
	if err := registerTaskQueueMetrics(metricStorage); err != nil {
		return fmt.Errorf("register task queue metrics: %w", err)
	}

	return nil
}

// registerConfigurationMetrics registers metrics related to configuration and bindings
func registerConfigurationMetrics(metricStorage metricsstorage.Storage) error {
	_, err := metricStorage.RegisterGauge(
		BindingCount,
		[]string{"module", pkg.MetricKeyHook},
		options.WithHelp("Number of bindings per module and hook"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", BindingCount, err)
	}

	_, err = metricStorage.RegisterCounter(
		ConfigValuesErrorsTotal,
		[]string{},
		options.WithHelp("Counter of ConfigMap validation errors"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConfigValuesErrorsTotal, err)
	}

	return nil
}

// registerModuleMetrics registers metrics related to module operations
func registerModuleMetrics(metricStorage metricsstorage.Storage) error {
	_, err := metricStorage.RegisterCounter(
		ModulesDiscoverErrorsTotal,
		[]string{},
		options.WithHelp("Counter of errors during module discovery"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModulesDiscoverErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(
		ModuleDeleteErrorsTotal,
		[]string{"module"},
		options.WithHelp("Counter of errors during module deletion"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleDeleteErrorsTotal, err)
	}

	_, err = metricStorage.RegisterHistogram(
		ModuleRunSeconds,
		[]string{"module", pkg.MetricKeyActivation},
		buckets_1msTo10s,
		options.WithHelp("Histogram of module execution times in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleRunSeconds, err)
	}

	_, err = metricStorage.RegisterCounter(
		ModuleRunErrorsTotal,
		[]string{"module"},
		options.WithHelp("Counter of module execution errors"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleRunErrorsTotal, err)
	}

	return nil
}

// registerModuleHookMetrics registers metrics related to module hook execution
func registerModuleHookMetrics(metricStorage metricsstorage.Storage) error {
	moduleHookLabels := []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}

	_, err := metricStorage.RegisterHistogram(
		ModuleHookRunSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of module hook execution times in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		ModuleHookRunUserCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of module hook user CPU usage in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunUserCPUSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		ModuleHookRunSysCPUSeconds,
		moduleHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of module hook system CPU usage in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunSysCPUSeconds, err)
	}

	_, err = metricStorage.RegisterGauge(
		ModuleHookRunMaxRSSBytes,
		moduleHookLabels,
		options.WithHelp("Gauge of maximum resident set size used by module hook in bytes"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookRunMaxRSSBytes, err)
	}

	_, err = metricStorage.RegisterCounter(
		ModuleHookAllowedErrorsTotal,
		moduleHookLabels,
		options.WithHelp("Counter of module hook execution errors that are allowed to fail (allowFailure: true)"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookAllowedErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(
		ModuleHookErrorsTotal,
		moduleHookLabels,
		options.WithHelp("Counter of module hook execution errors (allowFailure: false)"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(
		ModuleHookSuccessTotal,
		moduleHookLabels,
		options.WithHelp("Counter of successful module hook executions"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHookSuccessTotal, err)
	}

	return nil
}

// registerGlobalHookMetrics registers metrics related to global hook execution
func registerGlobalHookMetrics(metricStorage metricsstorage.Storage) error {
	globalHookLabels := []string{
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}

	_, err := metricStorage.RegisterHistogram(
		GlobalHookRunSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of global hook execution times in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		GlobalHookRunUserCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of global hook user CPU usage in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunUserCPUSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		GlobalHookRunSysCPUSeconds,
		globalHookLabels,
		buckets_1msTo10s,
		options.WithHelp("Histogram of global hook system CPU usage in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunSysCPUSeconds, err)
	}

	_, err = metricStorage.RegisterGauge(
		GlobalHookRunMaxRSSBytes,
		globalHookLabels,
		options.WithHelp("Gauge of maximum resident set size used by global hook in bytes"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookRunMaxRSSBytes, err)
	}

	_, err = metricStorage.RegisterCounter(
		GlobalHookAllowedErrorsTotal,
		globalHookLabels,
		options.WithHelp("Counter of global hook execution errors that are allowed to fail (allowFailure: true)"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookAllowedErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(
		GlobalHookErrorsTotal,
		globalHookLabels,
		options.WithHelp("Counter of global hook execution errors (allowFailure: false)"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookErrorsTotal, err)
	}

	_, err = metricStorage.RegisterCounter(
		GlobalHookSuccessTotal,
		globalHookLabels,
		options.WithHelp("Counter of successful global hook executions"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", GlobalHookSuccessTotal, err)
	}

	return nil
}

// registerConvergenceMetrics registers metrics related to convergence operations
func registerConvergenceMetrics(metricStorage metricsstorage.Storage) error {
	_, err := metricStorage.RegisterCounter(
		ConvergenceSeconds,
		[]string{pkg.MetricKeyActivation},
		options.WithHelp("Counter of convergence duration in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConvergenceSeconds, err)
	}

	_, err = metricStorage.RegisterCounter(
		ConvergenceTotal,
		[]string{pkg.MetricKeyActivation},
		options.WithHelp("Counter of convergence executions"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ConvergenceTotal, err)
	}

	return nil
}

// registerHelmMetrics registers metrics related to Helm operations
func registerHelmMetrics(metricStorage metricsstorage.Storage) error {
	_, err := metricStorage.RegisterHistogram(
		ModuleHelmSeconds,
		[]string{"module", pkg.MetricKeyActivation},
		buckets_1msTo10s,
		options.WithHelp("Histogram of Helm operation times for modules in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", ModuleHelmSeconds, err)
	}

	_, err = metricStorage.RegisterHistogram(
		HelmOperationSeconds,
		[]string{"module", pkg.MetricKeyActivation, "operation"},
		buckets_1msTo10s,
		options.WithHelp("Histogram of specific Helm operation durations in seconds"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", HelmOperationSeconds, err)
	}

	return nil
}

// registerTaskQueueMetrics registers metrics related to task queue operations
func registerTaskQueueMetrics(metricStorage metricsstorage.Storage) error {
	_, err := metricStorage.RegisterCounter(
		TaskWaitInQueueSecondsTotal,
		[]string{"module", pkg.MetricKeyHook, pkg.MetricKeyBinding, "queue"},
		options.WithHelp("Counter of seconds that tasks waited in queue before execution"),
	)
	if err != nil {
		return fmt.Errorf("can not register %s: %w", TaskWaitInQueueSecondsTotal, err)
	}

	return nil
}

// ============================================================================
// Live Metric Updaters
// ============================================================================

// StartLiveTicksUpdater starts a goroutine that periodically updates
// the live_ticks metric every 10 seconds.
// This metric can be used to verify that addon-operator is alive and functioning.
func StartLiveTicksUpdater(metricStorage metricsstorage.Storage) {
	// Register the live ticks counter
	_, _ = metricStorage.RegisterCounter(
		LiveTicks,
		[]string{},
		options.WithHelp("Counter that increases every 10 seconds to indicate addon-operator is alive"),
	)

	// Start the updater goroutine
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
	// Register the tasks queue length gauge
	_, _ = metricStorage.RegisterGauge(
		TasksQueueLength,
		[]string{"queue"},
		options.WithHelp("Gauge showing the length of the task queue"),
	)

	// Start the updater goroutine
	go func() {
		for {
			// Gather task queues lengths.
			tqs.IterateSnapshot(context.TODO(), func(_ context.Context, queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				metricStorage.GaugeSet(TasksQueueLength, queueLen, map[string]string{"queue": queue.Name})
			})

			time.Sleep(5 * time.Second)
		}
	}()
}
