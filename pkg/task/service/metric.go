package service

import (
	"time"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

// UpdateWaitInQueueMetric increases task_wait_in_queue_seconds_total counter for the task type.
// TODO pass queue name from handler, not from task
func (s *TaskHandlerService) UpdateWaitInQueueMetric(t sh_task.Task) {
	metricLabels := map[string]string{
		"module":             "",
		pkg.MetricKeyHook:    "",
		pkg.MetricKeyBinding: string(t.GetType()),
		"queue":              t.GetQueueName(),
	}

	hm := task.HookMetadataAccessor(t)

	switch t.GetType() {
	case task.GlobalHookRun,
		task.GlobalHookEnableScheduleBindings,
		task.GlobalHookEnableKubernetesBindings,
		task.GlobalHookWaitKubernetesSynchronization:
		metricLabels[pkg.MetricKeyHook] = hm.HookName

	case task.ModuleRun,
		task.ModuleDelete,
		task.ModuleHookRun,
		task.ModulePurge:
		metricLabels["module"] = hm.ModuleName

	case task.ConvergeModules,
		task.DiscoverHelmReleases:
		// no action required
	}

	if t.GetType() == task.GlobalHookRun {
		// set binding name instead of type
		metricLabels[pkg.MetricKeyBinding] = hm.Binding
	}
	if t.GetType() == task.ModuleHookRun {
		// set binding name instead of type
		metricLabels[pkg.MetricKeyHook] = hm.HookName
		metricLabels[pkg.MetricKeyBinding] = hm.Binding
	}

	taskWaitTime := time.Since(t.GetQueuedAt()).Seconds()
	s.metricStorage.CounterAdd(metrics.TaskWaitInQueueSecondsTotal, taskWaitTime, metricLabels)
}
