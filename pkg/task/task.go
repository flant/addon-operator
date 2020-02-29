package task

import (
	"github.com/flant/shell-operator/pkg/task"
)

// Addon-operator specific task types
const (
	ModuleDelete         task.TaskType = "ModuleDelete"
	ModuleRun            task.TaskType = "ModuleRun"
	ModuleHookRun        task.TaskType = "ModuleHookRun"
	GlobalHookRun        task.TaskType = "GlobalHookRun"
	DiscoverModulesState task.TaskType = "DiscoverModulesState"

	GlobalHookEnableKubernetesBindings task.TaskType = "GlobalHookEnableKubernetesBindings"
	GlobalHookEnableScheduleBindings   task.TaskType = "GlobalHookEnableScheduleBindings"
	//ModuleHookEnableKubernetesBindings      task.TaskType = "ModuleHookEnableKubernetesBindings"

	// Delete unknown helm release when no module in ModulesDir
	ModulePurge task.TaskType = "ModulePurge"
	// Task to call ModuleManager.Retry
	ModuleManagerRetry task.TaskType = "ModuleManagerRetry"
)
