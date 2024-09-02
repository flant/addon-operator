package task

import (
	"github.com/flant/shell-operator/pkg/task"
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
	// GroupedModuleRun runs beforeHelm/helm upgrade/afterHelm sequence for a group of modules.
	GroupedModuleRun task.TaskType = "GroupedModuleRun"
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
