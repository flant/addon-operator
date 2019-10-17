package task

import (
	"bytes"
	"fmt"
	"time"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/utils"
	"gopkg.in/satori/go.uuid.v1"
)

type TaskType string

const (
	ModuleDelete         TaskType = "ModuleDelete"
	ModuleRun            TaskType = "ModuleRun"
	ModuleHookRun        TaskType = "ModuleHookRun"
	GlobalHookRun        TaskType = "GlobalHookRun"
	DiscoverModulesState TaskType = "DiscoverModulesState"
	// удаление релиза без сведений о модуле
	ModulePurge TaskType = "ModulePurge"
	// retry module_manager-а
	ModuleManagerRetry TaskType = "ModuleManagerRetry"
	// вспомогательные задачи: задержка и остановка обработки
	Delay TaskType = "Delay"
	Stop  TaskType = "Stop"
)

type Task interface {
	GetName() string
	GetType() TaskType
	GetBinding() module_manager.BindingType
	GetBindingContext() []module_manager.BindingContext
	GetFailureCount() int
	IncrementFailureCount()
	GetDelay() time.Duration
	GetAllowFailure() bool
	GetOnStartupHooks() bool
	GetLogLabels() map[string]string
}

type BaseTask struct {
	FailureCount   int    // Failed executions count
	Name           string // Module or hook name
	Type           TaskType
	Binding        module_manager.BindingType
	BindingContext []module_manager.BindingContext
	Delay          time.Duration
	AllowFailure   bool // Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.

	OnStartupHooks bool // Run module onStartup hooks on Addon-operator startup or on module enabled.
	LogLabels map[string]string
}

var _ Task = &BaseTask{}

func NewTask(taskType TaskType, name string) *BaseTask {
	return &BaseTask{
		FailureCount:   0,
		Name:           name,
		Type:           taskType,
		AllowFailure:   false,
		BindingContext: make([]module_manager.BindingContext, 0),
		LogLabels: map[string]string{"task.id": uuid.NewV4().String()},
	}
}

func (t *BaseTask) GetName() string {
	return t.Name
}

func (t *BaseTask) GetType() TaskType {
	return t.Type
}

func (t *BaseTask) GetBinding() module_manager.BindingType {
	return t.Binding
}

func (t *BaseTask) GetBindingContext() []module_manager.BindingContext {
	return t.BindingContext
}

func (t *BaseTask) GetDelay() time.Duration {
	return t.Delay
}

func (t *BaseTask) GetAllowFailure() bool {
	return t.AllowFailure
}

func (t *BaseTask) GetOnStartupHooks() bool {
	return t.OnStartupHooks
}

func (t *BaseTask) GetLogLabels() map[string]string {
	return t.LogLabels
}

func (t *BaseTask) WithBinding(binding module_manager.BindingType) *BaseTask {
	t.Binding = binding
	return t
}

func (t *BaseTask) WithBindingContext(context []module_manager.BindingContext) *BaseTask {
	t.BindingContext = context
	return t
}

func (t *BaseTask) AppendBindingContext(context module_manager.BindingContext) *BaseTask {
	t.BindingContext = append(t.BindingContext, context)
	return t
}

func (t *BaseTask) WithAllowFailure(allowFailure bool) *BaseTask {
	t.AllowFailure = allowFailure
	return t
}

func (t *BaseTask) WithOnStartupHooks(onStartupHooks bool) *BaseTask {
	t.OnStartupHooks = onStartupHooks
	return t
}

func (t *BaseTask) WithLogLabels(labels map[string]string) *BaseTask {
	t.LogLabels = utils.MergeLabels(t.LogLabels, labels)
	return t
}

func (t *BaseTask) DumpAsText() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%s '%s'", t.Type, t.Name))
	if t.FailureCount > 0 {
		buf.WriteString(fmt.Sprintf(" failed %d times. ", t.FailureCount))
	}
	return buf.String()
}

func (t *BaseTask) GetFailureCount() int {
	return t.FailureCount
}

func (t *BaseTask) IncrementFailureCount() {
	t.FailureCount++
}

func NewTaskDelay(delay time.Duration) *BaseTask {
	return &BaseTask{
		Type:  Delay,
		Delay: delay,
	}
}
