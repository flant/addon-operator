// Package metrics provides centralized metric names and registration functions for addon-operator.
// All metric names use constants to ensure consistency and prevent typos.
// The {PREFIX} placeholder is replaced by the metrics storage with the appropriate prefix.
package metrics

import (
	"fmt"
	"time"

	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/deckhouse/deckhouse/pkg/metrics-storage/options"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// Metric name constants organized by functional area.
// Each constant represents a unique metric name used throughout addon-operator.
const (
	// ============================================================================
	// Common Metrics
	// ============================================================================
	// LiveTicks is a counter that increases every 10 seconds to indicate addon-operator is alive
	LiveTicks = "{PREFIX}live_ticks"

	// ============================================================================
	// Configuration and Binding Metrics
	// ============================================================================
	// BindingCount shows the number of hook bindings for each module
	BindingCount = "{PREFIX}binding_count"
	// ConfigValuesErrorsTotal counts configuration validation errors
	ConfigValuesErrorsTotal = "{PREFIX}config_values_errors_total"

	// ============================================================================
	// Module Lifecycle Metrics
	// ============================================================================
	// Module Discovery and Management
	ModulesDiscoverErrorsTotal        = "{PREFIX}modules_discover_errors_total"
	ModuleDeleteErrorsTotal           = "{PREFIX}module_delete_errors_total"
	ModulesHelmReleaseRedeployedTotal = "{PREFIX}modules_helm_release_redeployed_total"
	ModulesAbsentResourcesTotal       = "{PREFIX}modules_absent_resources_total"

	// Module Execution
	ModuleRunSeconds     = "{PREFIX}module_run_seconds"
	ModuleRunErrorsTotal = "{PREFIX}module_run_errors_total"

	// ============================================================================
	// Module Hook Execution Metrics
	// ============================================================================
	// Module Hook Runtime Metrics
	ModuleHookRunSeconds        = "{PREFIX}module_hook_run_seconds"
	ModuleHookRunUserCPUSeconds = "{PREFIX}module_hook_run_user_cpu_seconds"
	ModuleHookRunSysCPUSeconds  = "{PREFIX}module_hook_run_sys_cpu_seconds"
	ModuleHookRunMaxRSSBytes    = "{PREFIX}module_hook_run_max_rss_bytes"

	// Module Hook Execution Results
	ModuleHookErrorsTotal        = "{PREFIX}module_hook_errors_total"
	ModuleHookAllowedErrorsTotal = "{PREFIX}module_hook_allowed_errors_total"
	ModuleHookSuccessTotal       = "{PREFIX}module_hook_success_total"

	// ============================================================================
	// Global Hook Execution Metrics
	// ============================================================================
	// Global Hook Runtime Metrics
	GlobalHookRunSeconds        = "{PREFIX}global_hook_run_seconds"
	GlobalHookRunUserCPUSeconds = "{PREFIX}global_hook_run_user_cpu_seconds"
	GlobalHookRunSysCPUSeconds  = "{PREFIX}global_hook_run_sys_cpu_seconds"
	GlobalHookRunMaxRSSBytes    = "{PREFIX}global_hook_run_max_rss_bytes"

	// Global Hook Execution Results
	GlobalHookErrorsTotal        = "{PREFIX}global_hook_errors_total"
	GlobalHookAllowedErrorsTotal = "{PREFIX}global_hook_allowed_errors_total"
	GlobalHookSuccessTotal       = "{PREFIX}global_hook_success_total"

	// ============================================================================
	// Convergence and Synchronization Metrics
	// ============================================================================
	// ConvergenceSeconds tracks total time spent in convergence operations
	ConvergenceSeconds = "{PREFIX}convergence_seconds"
	// ConvergenceTotal counts the number of convergence operations completed
	ConvergenceTotal = "{PREFIX}convergence_total"

	// ============================================================================
	// Helm Operations Metrics
	// ============================================================================
	// ModuleHelmSeconds measures time taken for Helm operations on modules
	ModuleHelmSeconds = "{PREFIX}module_helm_seconds"
	// HelmOperationSeconds measures time for specific Helm operations
	HelmOperationSeconds = "{PREFIX}helm_operation_seconds"

	// ============================================================================
	// Task Queue Metrics
	// ============================================================================
	// TaskWaitInQueueSecondsTotal measures total time tasks wait in queue
	TaskWaitInQueueSecondsTotal = "{PREFIX}task_wait_in_queue_seconds_total"
	// TasksQueueLength shows the current number of pending tasks in each queue
	TasksQueueLength = "{PREFIX}tasks_queue_length"

	// ============================================================================
	// Module Manager Metrics
	// ============================================================================
	// ModuleManagerModuleInfo provides information about managed modules
	ModuleManagerModuleInfo = "{PREFIX}mm_module_info"
	// ModuleManagerModuleMaintenance tracks module maintenance status
	ModuleManagerModuleMaintenance = "{PREFIX}mm_module_maintenance"

	// Metric group names for grouped metrics
	ModuleInfoMetricGroup        = "mm_module_info"
	ModuleMaintenanceMetricGroup = "mm_module_maintenance"
)

// ============================================================================
// Common Configuration
// ============================================================================

// Standard histogram buckets for timing measurements from 1ms to 10s
// This provides good granularity for typical addon-operator operation durations
var defaultTimingBuckets = []float64{
	0.0,
	0.001, 0.002, 0.005, // 1, 2, 5 milliseconds
	0.01, 0.02, 0.05, // 10, 20, 50 milliseconds
	0.1, 0.2, 0.5, // 100, 200, 500 milliseconds
	1, 2, 5, // 1, 2, 5 seconds
	10, // 10 seconds
}

// Common label sets used across multiple metrics to ensure consistency
var (
	// moduleHookLabels are used for metrics tracking module hook execution
	moduleHookLabels = []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}

	// globalHookLabels are used for metrics tracking global hook execution
	globalHookLabels = []string{
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}

	// moduleLabels are used for metrics tracking module-level operations
	moduleLabels = []string{
		"module",
		pkg.MetricKeyActivation,
	}
)

// ============================================================================
// Registration Functions
// ============================================================================

// RegisterHookMetrics registers all hook-related metrics with the provided storage.
// This includes metrics for hook execution, module management, and system health.
// Used specifically by addon-operator's main registration function.
func RegisterHookMetrics(metricStorage metricsstorage.Storage) error {
	// Register configuration and binding metrics
	if err := registerConfigurationMetrics(metricStorage); err != nil {
		return fmt.Errorf("register configuration metrics: %w", err)
	}

	// Register module lifecycle metrics
	if err := registerModuleLifecycleMetrics(metricStorage); err != nil {
		return fmt.Errorf("register module lifecycle metrics: %w", err)
	}

	// Register module hook execution metrics
	if err := registerModuleHookExecutionMetrics(metricStorage); err != nil {
		return fmt.Errorf("register module hook execution metrics: %w", err)
	}

	// Register global hook execution metrics
	if err := registerGlobalHookExecutionMetrics(metricStorage); err != nil {
		return fmt.Errorf("register global hook execution metrics: %w", err)
	}

	// Register convergence metrics
	if err := registerConvergenceMetrics(metricStorage); err != nil {
		return fmt.Errorf("register convergence metrics: %w", err)
	}

	// Register Helm operation metrics
	if err := registerHelmOperationMetrics(metricStorage); err != nil {
		return fmt.Errorf("register helm operation metrics: %w", err)
	}

	// Register task queue metrics
	if err := registerTaskQueueMetrics(metricStorage); err != nil {
		return fmt.Errorf("register task queue metrics: %w", err)
	}

	return nil
}

// registerConfigurationMetrics registers metrics related to configuration and bindings
func registerConfigurationMetrics(metricStorage metricsstorage.Storage) error {
	bindingLabels := []string{"module", pkg.MetricKeyHook}

	_, err := metricStorage.RegisterGauge(
		BindingCount, bindingLabels,
		options.WithHelp("Gauge showing the number of bindings for each hook in the module"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", BindingCount, err)
	}

	_, err = metricStorage.RegisterCounter(
		ConfigValuesErrorsTotal, []string{},
		options.WithHelp("Counter of configuration validation errors"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", ConfigValuesErrorsTotal, err)
	}

	return nil
}

// registerModuleLifecycleMetrics registers metrics related to module discovery, management, and execution
func registerModuleLifecycleMetrics(metricStorage metricsstorage.Storage) error {
	moduleOnlyLabels := []string{"module"}

	// Module discovery and management counters
	lifecycleCounters := []struct {
		name   string
		labels []string
		help   string
	}{
		{ModulesDiscoverErrorsTotal, []string{}, "Counter of module discovery errors"},
		{ModuleDeleteErrorsTotal, moduleOnlyLabels, "Counter of module deletion errors"},
		{ModuleRunErrorsTotal, moduleOnlyLabels, "Counter of module execution errors"},
		{ModulesHelmReleaseRedeployedTotal, moduleOnlyLabels, "Counter of Helm releases that were redeployed"},
		{ModulesAbsentResourcesTotal, moduleOnlyLabels, "Counter of absent resources detected in modules"},
	}

	for _, counter := range lifecycleCounters {
		_, err := metricStorage.RegisterCounter(counter.name, counter.labels, options.WithHelp(counter.help))
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", counter.name, err)
		}
	}

	// Module execution duration histogram
	_, err := metricStorage.RegisterHistogram(
		ModuleRunSeconds, moduleLabels, defaultTimingBuckets,
		options.WithHelp("Histogram of module execution duration in seconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", ModuleRunSeconds, err)
	}

	return nil
}

// registerModuleHookExecutionMetrics registers metrics related to module hook execution and resource usage
func registerModuleHookExecutionMetrics(metricStorage metricsstorage.Storage) error {
	// Module hook timing metrics
	hookTimingMetrics := []struct {
		name string
		help string
	}{
		{ModuleHookRunSeconds, "Histogram of module hook execution duration in seconds"},
		{ModuleHookRunUserCPUSeconds, "Histogram of module hook user CPU usage in seconds"},
		{ModuleHookRunSysCPUSeconds, "Histogram of module hook system CPU usage in seconds"},
	}

	for _, metric := range hookTimingMetrics {
		_, err := metricStorage.RegisterHistogram(
			metric.name, moduleHookLabels, defaultTimingBuckets,
			options.WithHelp(metric.help),
		)
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", metric.name, err)
		}
	}

	// Module hook memory usage gauge
	_, err := metricStorage.RegisterGauge(
		ModuleHookRunMaxRSSBytes, moduleHookLabels,
		options.WithHelp("Gauge of maximum resident set size used by module hook in bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", ModuleHookRunMaxRSSBytes, err)
	}

	// Module hook execution result counters
	hookResultCounters := []struct {
		name string
		help string
	}{
		{ModuleHookErrorsTotal, "Counter of module hook execution errors (allowFailure: false)"},
		{ModuleHookAllowedErrorsTotal, "Counter of module hook execution errors that are allowed to fail (allowFailure: true)"},
		{ModuleHookSuccessTotal, "Counter of successful module hook executions"},
	}

	for _, counter := range hookResultCounters {
		_, err := metricStorage.RegisterCounter(counter.name, moduleHookLabels, options.WithHelp(counter.help))
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", counter.name, err)
		}
	}

	return nil
}

// registerGlobalHookExecutionMetrics registers metrics related to global hook execution and resource usage
func registerGlobalHookExecutionMetrics(metricStorage metricsstorage.Storage) error {
	// Global hook timing metrics
	globalHookTimingMetrics := []struct {
		name string
		help string
	}{
		{GlobalHookRunSeconds, "Histogram of global hook execution duration in seconds"},
		{GlobalHookRunUserCPUSeconds, "Histogram of global hook user CPU usage in seconds"},
		{GlobalHookRunSysCPUSeconds, "Histogram of global hook system CPU usage in seconds"},
	}

	for _, metric := range globalHookTimingMetrics {
		_, err := metricStorage.RegisterHistogram(
			metric.name, globalHookLabels, defaultTimingBuckets,
			options.WithHelp(metric.help),
		)
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", metric.name, err)
		}
	}

	// Global hook memory usage gauge
	_, err := metricStorage.RegisterGauge(
		GlobalHookRunMaxRSSBytes, globalHookLabels,
		options.WithHelp("Gauge of maximum resident set size used by global hook in bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", GlobalHookRunMaxRSSBytes, err)
	}

	// Global hook execution result counters
	globalHookResultCounters := []struct {
		name string
		help string
	}{
		{GlobalHookErrorsTotal, "Counter of global hook execution errors (allowFailure: false)"},
		{GlobalHookAllowedErrorsTotal, "Counter of global hook execution errors that are allowed to fail (allowFailure: true)"},
		{GlobalHookSuccessTotal, "Counter of successful global hook executions"},
	}

	for _, counter := range globalHookResultCounters {
		_, err := metricStorage.RegisterCounter(counter.name, globalHookLabels, options.WithHelp(counter.help))
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", counter.name, err)
		}
	}

	return nil
}

// registerConvergenceMetrics registers metrics related to convergence operations
func registerConvergenceMetrics(metricStorage metricsstorage.Storage) error {
	activationLabels := []string{pkg.MetricKeyActivation}

	convergenceMetrics := []struct {
		name string
		help string
	}{
		{ConvergenceSeconds, "Counter of total time spent in convergence operations in seconds"},
		{ConvergenceTotal, "Counter of convergence operations completed"},
	}

	for _, metric := range convergenceMetrics {
		_, err := metricStorage.RegisterCounter(metric.name, activationLabels, options.WithHelp(metric.help))
		if err != nil {
			return fmt.Errorf("failed to register %s: %w", metric.name, err)
		}
	}

	return nil
}

// registerHelmOperationMetrics registers metrics related to Helm operations
func registerHelmOperationMetrics(metricStorage metricsstorage.Storage) error {
	// Module Helm operations histogram
	_, err := metricStorage.RegisterHistogram(
		ModuleHelmSeconds, moduleLabels, defaultTimingBuckets,
		options.WithHelp("Histogram of Helm operation duration on modules in seconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", ModuleHelmSeconds, err)
	}

	// Specific Helm operations histogram with operation type
	helmOperationLabels := []string{"module", pkg.MetricKeyActivation, "operation"}
	_, err = metricStorage.RegisterHistogram(
		HelmOperationSeconds, helmOperationLabels, defaultTimingBuckets,
		options.WithHelp("Histogram of specific Helm operation durations in seconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", HelmOperationSeconds, err)
	}

	return nil
}

// registerTaskQueueMetrics registers metrics related to task queue operations
func registerTaskQueueMetrics(metricStorage metricsstorage.Storage) error {
	// Task queue wait time counter
	taskLabels := []string{"module", pkg.MetricKeyHook, pkg.MetricKeyBinding, "queue"}
	_, err := metricStorage.RegisterCounter(
		TaskWaitInQueueSecondsTotal, taskLabels,
		options.WithHelp("Counter of seconds that tasks waited in queue before execution"),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s: %w", TaskWaitInQueueSecondsTotal, err)
	}

	return nil
}

// ============================================================================
// Background Metric Updaters
// ============================================================================

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
