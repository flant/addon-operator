package task

import (
	"context"

	sh_task "github.com/flant/shell-operator/pkg/task"
)

// Addon-operator specific task types
const (
	// GlobalHookRun runs a global hook.
	GlobalHookRun sh_task.TaskType = "GlobalHookRun"
	// ModuleHookRun runs schedule or kubernetes hook.
	ModuleHookRun sh_task.TaskType = "ModuleHookRun"
	// ModuleDelete runs helm delete/afterHelmDelete sequence.
	ModuleDelete sh_task.TaskType = "ModuleDelete"
	// ModuleRun runs beforeHelm/helm upgrade/afterHelm sequence.
	ModuleRun sh_task.TaskType = "ModuleRun"
	// ParallelModuleRun runs beforeHelm/helm upgrade/afterHelm sequence for a bunch of modules in parallel.
	ParallelModuleRun sh_task.TaskType = "ParallelModuleRun"
	// ModulePurge - delete unknown helm release (no module in ModulesDir)
	ModulePurge sh_task.TaskType = "ModulePurge"
	// ModuleEnsureCRDs runs ensureCRDs task for enabled module
	ModuleEnsureCRDs sh_task.TaskType = "ModuleEnsureCRDs"

	// DiscoverHelmReleases lists helm releases to detect unknown modules and initiate enabled modules list.
	DiscoverHelmReleases sh_task.TaskType = "DiscoverHelmReleases"

	// ConvergeModules runs beforeAll/run modules/afterAll sequence for all enabled modules.
	ConvergeModules sh_task.TaskType = "ConvergeModules"

	// ApplyKubeConfigValues validates and updates modules' values
	ApplyKubeConfigValues sh_task.TaskType = "ApplyKubeConfigValues"

	GlobalHookEnableKubernetesBindings      sh_task.TaskType = "GlobalHookEnableKubernetesBindings"
	GlobalHookWaitKubernetesSynchronization sh_task.TaskType = "GlobalHookWaitKubernetesSynchronization"
	GlobalHookEnableScheduleBindings        sh_task.TaskType = "GlobalHookEnableScheduleBindings"
)

type Task interface {
	Handle(ctx context.Context) sh_task.Result
}
