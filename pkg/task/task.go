package task

import (
	"time"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/task"
)

// Define additional task types
const (
	ModuleDelete         task.TaskType = "ModuleDelete"
	ModuleRun            task.TaskType = "ModuleRun"
	ModuleHookRun        task.TaskType = "ModuleHookRun"
	GlobalHookRun        task.TaskType = "GlobalHookRun"
	DiscoverModulesState task.TaskType = "DiscoverModulesState"

	GlobalKubernetesBindingsStart task.TaskType = "GlobalKubernetesBindingsStart"
	ModuleKubernetesBindingsStart task.TaskType = "ModuleKubernetesBindingsStart"

	// удаление релиза без сведений о модуле
	ModulePurge task.TaskType = "ModulePurge"
	// retry module_manager-а
	ModuleManagerRetry task.TaskType = "ModuleManagerRetry"
)

type Task interface {
	task.Task

	GetOnStartupHooks() bool
}

type BaseTask struct {
	task.BaseTask

	OnStartupHooks bool // Run module onStartup hooks on Addon-operator startup or on module enabled.
}

var _ Task = &BaseTask{}

func NewTask(taskType task.TaskType, name string) *BaseTask {
	return &BaseTask{
		BaseTask: *task.NewTask(taskType, name),
	}
}

func (t *BaseTask) WithBinding(binding BindingType) *BaseTask {
	t.BaseTask.WithBinding(binding)
	return t
}

func (t *BaseTask) WithBindingContext(context []BindingContext) *BaseTask {
	t.BaseTask.WithBindingContext(context)
	return t
}

func (t *BaseTask) AppendBindingContext(context BindingContext) *BaseTask {
	t.BaseTask.AppendBindingContext(context)
	return t
}

func (t *BaseTask) WithAllowFailure(allowFailure bool) *BaseTask {
	t.BaseTask.WithAllowFailure(allowFailure)
	return t
}

func (t *BaseTask) WithLogLabels(labels map[string]string) *BaseTask {
	t.BaseTask.WithLogLabels(labels)
	return t
}


func (t *BaseTask) GetOnStartupHooks() bool {
	return t.OnStartupHooks
}

func (t *BaseTask) WithOnStartupHooks(onStartupHooks bool) *BaseTask {
	t.OnStartupHooks = onStartupHooks
	return t
}

func NewTaskDelay(delay time.Duration) *BaseTask {
	return &BaseTask{
		BaseTask: *task.NewTaskDelay(delay),
	}
}
