package metrics

import (
	"context"
	"testing"

	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// newTestTask creates a BaseTask with given type, moduleName, and hookName labels.
// Note: metadata is stored as opaque interface{}; extraction is done by the caller's callback.
func newTestTask(taskType sh_task.TaskType) sh_task.Task {
	return sh_task.NewTask(taskType)
}

// setupTestStorage creates a MetricStorage backed by a fresh prometheus registry
// and registers the tasks_queue_head_info gauge.
func setupTestStorage(t *testing.T) (metricsstorage.Storage, prometheus.Gatherer) {
	t.Helper()
	registry := prometheus.NewRegistry()
	storage := metricsstorage.NewMetricStorage(
		metricsstorage.WithRegistry(registry),
	)
	_, err := storage.RegisterGauge(
		TasksQueueHeadInfo,
		[]string{pkg.MetricKeyQueue, "module", "task_type", "hook"},
	)
	require.NoError(t, err)
	return storage, registry
}

// setupTestQueueSet creates a TaskQueueSet with a single "main" queue and one task in it.
// Returns the queue set and the main queue for adding more tasks.
func setupTestQueueSet(t *testing.T, storage metricsstorage.Storage) (*queue.TaskQueueSet, *queue.TaskQueue) {
	t.Helper()
	tqs := queue.NewTaskQueueSet()
	// Provide a context for queue internals (WithContext in queue options).
	tqs.WithContext(context.Background())
	// Provide storage so queue internals don't panic on nil metric storage access.
	tqs.WithMetricStorage(storage)
	tqs.NewNamedQueue("main", nil)
	mainQ := tqs.GetByName("main")
	require.NotNil(t, mainQ)
	return tqs, mainQ
}

// findHeadInfoMetric returns the tasks_queue_head_info metric family from gathered metrics.
func findHeadInfoMetric(t *testing.T, gatherer prometheus.Gatherer) []*dto.Metric {
	t.Helper()
	mfs, err := gatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == TasksQueueHeadInfo {
			return mf.GetMetric()
		}
	}
	return nil
}

// labelMap converts *dto.LabelPair slice to a map for easy assertions.
func labelMap(m *dto.Metric) map[string]string {
	result := make(map[string]string)
	for _, lp := range m.GetLabel() {
		result[lp.GetName()] = lp.GetValue()
	}
	return result
}

// staticHeadExtractor returns a head info extractor that always returns the given values.
func staticHeadExtractor(module, hook string) func(interface{}) (string, string) {
	return func(_ interface{}) (string, string) {
		return module, hook
	}
}

func TestUpdateTasksQueueHeadInfo_EmptyQueue_NoSeries(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, _ := setupTestQueueSet(t, storage)

	extractor := staticHeadExtractor("module", "hook")
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics := findHeadInfoMetric(t, gatherer)
	assert.Nil(t, metrics, "empty queue should produce no head_info series")
}

func TestUpdateTasksQueueHeadInfo_NonEmptyQueue_OneSeries(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, mainQ := setupTestQueueSet(t, storage)

	task := newTestTask(sh_task.TaskType("ModuleRun"))
	mainQ.AddLast(task)

	extractor := staticHeadExtractor("test-module", "test-hook")
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1)
	labels := labelMap(metrics[0])
	assert.Equal(t, "main", labels[pkg.MetricKeyQueue])
	assert.Equal(t, "test-module", labels["module"])
	assert.Equal(t, "ModuleRun", labels["task_type"])
	assert.Equal(t, "test-hook", labels["hook"])
	assert.Equal(t, float64(1), metrics[0].GetGauge().GetValue())
}

func TestUpdateTasksQueueHeadInfo_ParallelModuleRunNormalization(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, mainQ := setupTestQueueSet(t, storage)

	task := newTestTask(sh_task.TaskType("ParallelModuleRun"))
	mainQ.AddLast(task)

	// Simulate ParallelModuleRun synthetic module name
	extractor := staticHeadExtractor("Parallel run for module-a, module-b", "")
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1)
	labels := labelMap(metrics[0])
	assert.Equal(t, "", labels["module"], "ParallelModuleRun synthetic name should be normalized to empty string")
	assert.Equal(t, "ParallelModuleRun", labels["task_type"])
}

func TestUpdateTasksQueueHeadInfo_GlobalTask_EmptyModule(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, mainQ := setupTestQueueSet(t, storage)

	task := newTestTask(sh_task.TaskType("ConvergeModules"))
	mainQ.AddLast(task)

	// Global tasks have empty ModuleName
	extractor := staticHeadExtractor("", "")
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1)
	labels := labelMap(metrics[0])
	assert.Equal(t, "", labels["module"], "global task should have empty module")
	assert.Equal(t, "", labels["hook"], "global task should have empty hook")
	assert.Equal(t, "ConvergeModules", labels["task_type"])
}

func TestUpdateTasksQueueHeadInfo_HeadChange_OldSeriesExpired(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, mainQ := setupTestQueueSet(t, storage)

	// First: add task with "module-a"
	taskA := newTestTask(sh_task.TaskType("ModuleRun"))
	mainQ.AddLast(taskA)

	extractorA := staticHeadExtractor("module-a", "hook-a")
	updateTasksQueueHeadInfo(tqs, storage, extractorA)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1, "first head should produce one series")
	assert.Equal(t, "module-a", labelMap(metrics[0])["module"])

	// Second: remove first task and add new task with "module-b"
	mainQ.RemoveFirst()
	taskB := newTestTask(sh_task.TaskType("ModuleRun"))
	mainQ.AddLast(taskB)

	extractorB := staticHeadExtractor("module-b", "hook-b")
	updateTasksQueueHeadInfo(tqs, storage, extractorB)

	metrics = findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1, "after head change, only one active series should remain")
	labels := labelMap(metrics[0])
	assert.Equal(t, "module-b", labels["module"], "new head should be module-b")
	assert.Equal(t, "hook-b", labels["hook"], "new head should be hook-b")
	// Verify old module-a is gone
	assert.NotEqual(t, "module-a", labels["module"], "old head series should be expired")
}

func TestUpdateTasksQueueHeadInfo_MultipleQueues_CorrectLabels(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs := queue.NewTaskQueueSet()
	tqs.WithContext(context.Background())
	tqs.WithMetricStorage(storage)
	tqs.NewNamedQueue("main", nil)
	tqs.NewNamedQueue("secondary", nil)

	mainQ := tqs.GetByName("main")
	secQ := tqs.GetByName("secondary")

	mainQ.AddLast(newTestTask(sh_task.TaskType("ModuleRun")))
	secQ.AddLast(newTestTask(sh_task.TaskType("ModuleHookRun")))

	// Use different extractors per queue — simulate real behavior via type switch
	// In real code, the extractor is shared. We test with a shared static extractor
	// that can differentiate based on task type.
	sharedExtractor := staticHeadExtractor("shared-module", "shared-hook")
	updateTasksQueueHeadInfo(tqs, storage, sharedExtractor)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 2, "two non-empty queues should produce two series")

	// Collect queue names from metrics
	queueNames := make(map[string]bool)
	for _, m := range metrics {
		queueNames[labelMap(m)[pkg.MetricKeyQueue]] = true
	}
	assert.True(t, queueNames["main"], "main queue should be present")
	assert.True(t, queueNames["secondary"], "secondary queue should be present")
}

func TestUpdateTasksQueueHeadInfo_QueueEmptyAfterHead_Removed(t *testing.T) {
	storage, gatherer := setupTestStorage(t)
	tqs, mainQ := setupTestQueueSet(t, storage)

	// First: add a task, run updater
	mainQ.AddLast(newTestTask(sh_task.TaskType("ModuleRun")))
	extractor := staticHeadExtractor("some-module", "some-hook")
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics := findHeadInfoMetric(t, gatherer)
	require.Len(t, metrics, 1, "should have one series initially")

	// Remove the task, making queue empty
	mainQ.RemoveFirst()
	updateTasksQueueHeadInfo(tqs, storage, extractor)

	metrics = findHeadInfoMetric(t, gatherer)
	assert.Nil(t, metrics, "after queue becomes empty, all head_info series should be expired")
}

func TestTasksQueueHeadInfo_ConstantPrefix(t *testing.T) {
	// In tests, ReplacePrefix is not called (that happens at app bootstrap).
	// The variables contain "{PREFIX}..." as placeholders.
	// Verify the variables exist and are initialized.
	assert.NotEmpty(t, TasksQueueLength)
	assert.NotEmpty(t, TasksQueueHeadInfo)
	assert.Contains(t, TasksQueueHeadInfo, "{PREFIX}", "should contain placeholder before ReplacePrefix")
}

func TestTasksQueueLength_ConstantPrefix(t *testing.T) {
	// TasksQueueLength is initialized to "{PREFIX}..." in package init.
	// After ReplacePrefix is called (which happens at app startup), it gets the real prefix.
	// In tests, ReplacePrefix is typically called by the app bootstrap.
	// We just verify the variable exists and is a non-empty string.
	assert.NotEmpty(t, TasksQueueLength)
}

// Ensure the package init doesn't panic due to uninitialized variables.
func TestPackageInit_NoPanic(t *testing.T) {
	assert.NotEmpty(t, TasksQueueHeadInfo)
	assert.NotEmpty(t, ModuleInfoMetricName)
}

// Test that ExpireGroupMetricByName handles unregistered metrics gracefully.
func TestExpireGroupMetricByName_UnregisteredMetric_NoPanic(t *testing.T) {
	registry := prometheus.NewRegistry()
	storage := metricsstorage.NewMetricStorage(
		metricsstorage.WithRegistry(registry),
	)
	// Calling ExpireGroupMetricByName on an unregistered metric should not panic.
	assert.NotPanics(t, func() {
		storage.Grouped().ExpireGroupMetricByName("nonexistent", "nonexistent_metric")
	})
}
