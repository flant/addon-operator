package helpers

import (
	"fmt"
	"strings"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/task"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

func IsOperatorStartupTask(t sh_task.Task) bool {
	taskLogLabels := t.GetLogLabels()

	eventType, ok := taskLogLabels[pkg.LogKeyEventType]
	if ok && eventType == converge.OperatorStartup.String() {
		return true
	}

	return false
}

// TaskDescriptionForTaskFlowLog generates a human-readable description of a task
// for logging purposes.
func TaskDescriptionForTaskFlowLog(tsk sh_task.Task, action string, phase string, status string) string {
	hm := task.HookMetadataAccessor(tsk)
	taskType := string(tsk.GetType())

	// Start building the description
	description := formatTaskAction(taskType, action, status)

	// Format task-specific details based on task type
	details := formatTaskDetails(tsk, hm, phase)

	// Add trigger information if available
	triggerInfo := ""
	if hm.EventDescription != "" {
		triggerInfo = fmt.Sprintf(", trigger is %s", hm.EventDescription)
	}

	return description + details + triggerInfo
}

// formatTaskAction creates the first part of the task description
func formatTaskAction(taskType, action, status string) string {
	switch action {
	case "start":
		return fmt.Sprintf("%s task", taskType)
	case "end":
		return fmt.Sprintf("%s task done, result is '%s'", taskType, status)
	default:
		return fmt.Sprintf("%s task %s", action, taskType)
	}
}

// formatTaskDetails creates the task-specific part of the description
func formatTaskDetails(tsk sh_task.Task, hm task.HookMetadata, phase string) string {
	switch tsk.GetType() {
	case task.GlobalHookRun, task.ModuleHookRun:
		return formatHookTaskDetails(hm)

	case task.ConvergeModules:
		return formatConvergeTaskDetails(tsk, phase)

	case task.ModuleRun:
		details := fmt.Sprintf(" for module '%s', phase '%s'", hm.ModuleName, phase)
		if hm.DoModuleStartup {
			details += " with doModuleStartup"
		}
		return details

	case task.ParallelModuleRun:
		return fmt.Sprintf(" for modules '%s'", hm.ModuleName)

	case task.ModulePurge, task.ModuleDelete, task.ModuleEnsureCRDs:
		return fmt.Sprintf(" for module '%s'", hm.ModuleName)

	case task.GlobalHookEnableKubernetesBindings,
		task.GlobalHookWaitKubernetesSynchronization,
		task.GlobalHookEnableScheduleBindings:
		return " for the hook"

	case task.DiscoverHelmReleases:
		return ""

	default:
		return ""
	}
}

// formatHookTaskDetails formats details specific to hook tasks
func formatHookTaskDetails(hm task.HookMetadata) string {
	// Handle case with no binding contexts
	if len(hm.BindingContext) == 0 {
		return " for no binding"
	}

	var sb strings.Builder
	sb.WriteString(" for ")

	// Get primary binding context
	bc := hm.BindingContext[0]

	// Check if this is a synchronization task
	if bc.IsSynchronization() {
		sb.WriteString("Synchronization of ")
	}

	// Format binding information based on type and group
	bindingType := bc.Metadata.BindingType
	bindingGroup := bc.Metadata.Group

	if bindingGroup == "" {
		// No group specified, format based on binding type
		if bindingType == htypes.OnKubernetesEvent || bindingType == htypes.Schedule {
			fmt.Fprintf(&sb, "'%s/%s'", bindingType, bc.Binding)
		} else {
			sb.WriteString(string(bindingType))
		}
	} else {
		// Use group information
		fmt.Fprintf(&sb, "'%s' group", bindingGroup)
	}

	// Add information about additional bindings
	if len(hm.BindingContext) > 1 {
		fmt.Fprintf(&sb, " and %d more bindings", len(hm.BindingContext)-1)
	} else {
		sb.WriteString(" binding")
	}

	return sb.String()
}

// formatConvergeTaskDetails formats details specific to converge tasks
func formatConvergeTaskDetails(tsk sh_task.Task, phase string) string {
	if taskEvent, ok := tsk.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent); ok {
		return fmt.Sprintf(" for %s in phase '%s'", string(taskEvent), phase)
	}
	return ""
}
