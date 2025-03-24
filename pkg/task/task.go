package task

import (
	"context"

	"github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// Addon-operator specific task types
const (
	// GlobalHookRun runs a global hook.
	GlobalHookRun task.TaskType = "GlobalHookRun"
	// ModuleHookRun runs schedule or kubernetes hook.
	ModuleHookRun task.TaskType = "ModuleHookRun"
	// ModuleDelete runs helm delete/afterHelmDelete sequence.
	ModuleDelete task.TaskType = "ModuleDelete"
	// ModuleRun runs beforeHelm/helm upgrade/afterHelm sequence.
	ModuleRun task.TaskType = "ModuleRun"
	// ParallelModuleRun runs beforeHelm/helm upgrade/afterHelm sequence for a bunch of modules in parallel.
	ParallelModuleRun task.TaskType = "ParallelModuleRun"
	// ModulePurge - delete unknown helm release (no module in ModulesDir)
	ModulePurge task.TaskType = "ModulePurge"
	// ModuleEnsureCRDs runs ensureCRDs task for enabled module
	ModuleEnsureCRDs task.TaskType = "ModuleEnsureCRDs"

	// DiscoverHelmReleases lists helm releases to detect unknown modules and initiate enabled modules list.
	DiscoverHelmReleases task.TaskType = "DiscoverHelmReleases"

	// ConvergeModules runs beforeAll/run modules/afterAll sequence for all enabled modules.
	ConvergeModules task.TaskType = "ConvergeModules"

	// ApplyKubeConfigValues validates and updates modules' values
	ApplyKubeConfigValues task.TaskType = "ApplyKubeConfigValues"

	GlobalHookEnableKubernetesBindings      task.TaskType = "GlobalHookEnableKubernetesBindings"
	GlobalHookWaitKubernetesSynchronization task.TaskType = "GlobalHookWaitKubernetesSynchronization"
	GlobalHookEnableScheduleBindings        task.TaskType = "GlobalHookEnableScheduleBindings"
)

type Task interface {
	Handle(ctx context.Context) queue.TaskResult
}

type TaskInternals struct {
	metricLabels map[string]string
}

func (i *TaskInternals) GetMetricLabels() map[string]string {
	return i.metricLabels
}

func (i *TaskInternals) AddMetricLabel(key, value string) {
	if i.metricLabels == nil {
		i.metricLabels = make(map[string]string)
	}

	i.metricLabels[key] = value
}

func (i *TaskInternals) DeleteMetricLabel(key string) {
	delete(i.metricLabels, key)
}
