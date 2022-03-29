package addon_operator

import (
	"time"

	"github.com/flant/shell-operator/pkg/metric_storage"
	sh_op "github.com/flant/shell-operator/pkg/shell-operator"
	"github.com/flant/shell-operator/pkg/task/queue"
)

func RegisterAddonOperatorMetrics(metricStorage *metric_storage.MetricStorage) {
	sh_op.RegisterCommonMetrics(metricStorage)
	sh_op.RegisterTaskQueueMetrics(metricStorage)
	sh_op.RegisterKubeEventsManagerMetrics(metricStorage, map[string]string{
		"module":  "",
		"hook":    "",
		"binding": "",
		"queue":   "",
		"kind":    "",
	})
	RegisterHookMetrics(metricStorage)
}

var buckets_1msTo10s = []float64{
	0.0,
	0.001, 0.002, 0.005, // 1,2,5 milliseconds
	0.01, 0.02, 0.05, // 10,20,50 milliseconds
	0.1, 0.2, 0.5, // 100,200,500 milliseconds
	1, 2, 5, // 1,2,5 seconds
	10, // 10 seconds
}

func RegisterHookMetrics(metricStorage *metric_storage.MetricStorage) {
	// configuration metrics
	metricStorage.RegisterGauge(
		"{PREFIX}binding_count",
		map[string]string{
			"module": "",
			"hook":   "",
		})
	// ConfigMap validation errors
	metricStorage.RegisterCounter("{PREFIX}config_values_errors_total", map[string]string{})

	// modules
	metricStorage.RegisterCounter("{PREFIX}modules_discover_errors_total", map[string]string{})
	metricStorage.RegisterCounter("{PREFIX}module_delete_errors_total", map[string]string{"module": ""})

	// module
	metricStorage.RegisterHistogram(
		"{PREFIX}module_run_seconds",
		map[string]string{
			"module":     "",
			"activation": "",
		},
		buckets_1msTo10s,
	)
	metricStorage.RegisterCounter("{PREFIX}module_run_errors_total", map[string]string{"module": ""})

	moduleHookLabels := map[string]string{
		"module":     "",
		"hook":       "",
		"binding":    "",
		"queue":      "",
		"activation": "",
	}
	metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_user_cpu_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterHistogram(
		"{PREFIX}module_hook_run_sys_cpu_seconds",
		moduleHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterGauge("{PREFIX}module_hook_run_max_rss_bytes", moduleHookLabels)
	metricStorage.RegisterCounter("{PREFIX}module_hook_allowed_errors_total", moduleHookLabels)
	metricStorage.RegisterCounter("{PREFIX}module_hook_errors_total", moduleHookLabels)
	metricStorage.RegisterCounter("{PREFIX}module_hook_success_total", moduleHookLabels)

	// global hook running
	globalHookLabels := map[string]string{
		"hook":       "",
		"binding":    "",
		"queue":      "",
		"activation": "",
	}
	metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_user_cpu_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterHistogram(
		"{PREFIX}global_hook_run_sys_cpu_seconds",
		globalHookLabels,
		buckets_1msTo10s)
	metricStorage.RegisterGauge("{PREFIX}global_hook_run_max_rss_bytes", globalHookLabels)
	metricStorage.RegisterCounter("{PREFIX}global_hook_allowed_errors_total", globalHookLabels)
	metricStorage.RegisterCounter("{PREFIX}global_hook_errors_total", globalHookLabels)
	metricStorage.RegisterCounter("{PREFIX}global_hook_success_total", globalHookLabels)

	// converge duration
	metricStorage.RegisterCounter("{PREFIX}convergence_seconds", map[string]string{"activation": ""})
	metricStorage.RegisterCounter("{PREFIX}convergence_total", map[string]string{"activation": ""})

	// helm operations
	metricStorage.RegisterHistogram(
		"{PREFIX}module_helm_seconds",
		map[string]string{
			"module":     "",
			"activation": "",
		},
		buckets_1msTo10s)
	metricStorage.RegisterHistogram(
		"{PREFIX}helm_operation_seconds",
		map[string]string{
			"module":     "",
			"activation": "",
			"operation":  "",
		},
		buckets_1msTo10s)

	// task age
	// hook_run task waiting time
	metricStorage.RegisterCounter(
		"{PREFIX}task_wait_in_queue_seconds_total",
		map[string]string{
			"module":  "",
			"hook":    "",
			"binding": "",
			"queue":   "",
		})
}

func StartLiveTicksUpdater(metricStorage *metric_storage.MetricStorage) {
	// Addon-operator live ticks.
	go func() {
		for {
			metricStorage.CounterAdd("{PREFIX}live_ticks", 1.0, map[string]string{})
			time.Sleep(10 * time.Second)
		}
	}()
}

func StartTasksQueueLengthUpdater(metricStorage *metric_storage.MetricStorage, tqs *queue.TaskQueueSet) {
	go func() {
		for {
			// Gather task queues lengths.
			tqs.Iterate(func(queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				metricStorage.GaugeSet("{PREFIX}tasks_queue_length", queueLen, map[string]string{"queue": queue.Name})
			})
			time.Sleep(5 * time.Second)
		}
	}()
}
