package addon_operator

import (
	"context"
	"time"

	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/shell-operator/pkg/task/queue"
)

var buckets_1msTo10s = []float64{
	0.0,
	0.001, 0.002, 0.005, // 1,2,5 milliseconds
	0.01, 0.02, 0.05, // 10,20,50 milliseconds
	0.1, 0.2, 0.5, // 100,200,500 milliseconds
	1, 2, 5, // 1,2,5 seconds
	10, // 10 seconds
}

// registerHookMetrics register metrics specified for addon-operator
func registerHookMetrics(metricStorage metricsstorage.Storage) {
	// configuration metrics
	_, _ = metricStorage.RegisterGauge(
		"{PREFIX}binding_count",
		[]string{
			"module",
			pkg.MetricKeyHook,
		})
	// ConfigMap validation errors
	_, _ = metricStorage.RegisterCounter("{PREFIX}config_values_errors_total", []string{})

	// modules
	_, _ = metricStorage.RegisterCounter("{PREFIX}modules_discover_errors_total", []string{})
	_, _ = metricStorage.RegisterCounter("{PREFIX}module_delete_errors_total", []string{"module"})

	// module
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}module_run_seconds",
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s,
	)
	_, _ = metricStorage.RegisterCounter("{PREFIX}module_run_errors_total", []string{"module"})

	moduleHookLabels := []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_user_cpu_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_sys_cpu_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterGauge("{PREFIX}module_hook_run_max_rss_bytes", moduleHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}module_hook_allowed_errors_total", moduleHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}module_hook_errors_total", moduleHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}module_hook_success_total", moduleHookLabels)

	// global hook running
	globalHookLabels := []string{
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		pkg.MetricKeyActivation,
	}
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_user_cpu_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_sys_cpu_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterGauge("{PREFIX}global_hook_run_max_rss_bytes", globalHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}global_hook_allowed_errors_total", globalHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}global_hook_errors_total", globalHookLabels)
	_, _ = metricStorage.RegisterCounter("{PREFIX}global_hook_success_total", globalHookLabels)

	// converge duration
	_, _ = metricStorage.RegisterCounter("{PREFIX}convergence_seconds", []string{pkg.MetricKeyActivation})
	_, _ = metricStorage.RegisterCounter("{PREFIX}convergence_total", []string{pkg.MetricKeyActivation})

	// helm operations
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}module_helm_seconds",
		[]string{
			"module",
			pkg.MetricKeyActivation,
		},
		buckets_1msTo10s)
	_, _ = metricStorage.RegisterHistogram(
		"{PREFIX}helm_operation_seconds",
		[]string{
			"module",
			pkg.MetricKeyActivation,
			"operation",
		},
		buckets_1msTo10s)

	// task age
	// hook_run task waiting time
	_, _ = metricStorage.RegisterCounter(
		"{PREFIX}task_wait_in_queue_seconds_total",
		[]string{
			"module",
			pkg.MetricKeyHook,
			pkg.MetricKeyBinding,
			"queue",
		})
}

// StartLiveTicksUpdater starts a goroutine that periodically updates
// the live_ticks metric every 10 seconds.
// This metric can be used to verify that addon-operator is alive and functioning.
func StartLiveTicksUpdater(metricStorage metricsstorage.Storage) {
	// Addon-operator live ticks.
	go func() {
		for {
			metricStorage.CounterAdd("{PREFIX}live_ticks", 1.0, map[string]string{})

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
			tqs.IterateSnapshot(context.TODO(), func(_ context.Context, queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				metricStorage.GaugeSet("{PREFIX}tasks_queue_length", queueLen, map[string]string{"queue": queue.Name})
			})

			time.Sleep(5 * time.Second)
		}
	}()
}
