package schedule

import (
	"fmt"

	"github.com/flant/shell-operator/pkg/hook"
	"github.com/romana/rlog"

	"github.com/flant/shell-operator/pkg/hook/schedule"
	"github.com/flant/shell-operator/pkg/schedule_manager"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
)

type ScheduleHooksController interface {
	WithModuleManager(moduleManager module_manager.ModuleManager)
	WithScheduleManager(schedule_manager.ScheduleManager)

	UpdateScheduleHooks()

	HandleEvent(crontab string) ([]task.Task, error)
}

type scheduleHooksController struct {
	GlobalHooks schedule.ScheduleHooksStorage
	ModuleHooks schedule.ScheduleHooksStorage

	// dependencies
	moduleManager   module_manager.ModuleManager
	scheduleManager schedule_manager.ScheduleManager
}

var NewScheduleHooksController = func() ScheduleHooksController {
	return &scheduleHooksController{
		GlobalHooks: make(schedule.ScheduleHooksStorage, 0),
		ModuleHooks: make(schedule.ScheduleHooksStorage, 0),
	}
}

func (c *scheduleHooksController) WithModuleManager(moduleManager module_manager.ModuleManager) {
	c.moduleManager = moduleManager
}

func (c *scheduleHooksController) WithScheduleManager(scheduleManager schedule_manager.ScheduleManager) {
	c.scheduleManager = scheduleManager
}

// UpdateScheduledHooks recreates a new Hooks array. Note that many hooks can be bind
// to one crontab.
func (c *scheduleHooksController) UpdateScheduleHooks() {
	// Load active crontabs for global hooks
	newGlobalHooks := make(schedule.ScheduleHooksStorage, 0)
	globalHooks := c.moduleManager.GetGlobalHooksInOrder(module_manager.Schedule)

	for _, hookName := range globalHooks {
		globalHook, _ := c.moduleManager.GetGlobalHook(hookName)
		for _, scheduleCfg := range globalHook.Config.Schedules {
			_, err := c.scheduleManager.Add(scheduleCfg.Crontab)
			if err != nil {
				rlog.Errorf("Schedule: cannot add '%s' for hook '%s': %s", scheduleCfg.Crontab, hookName, err)
				continue
			}
			rlog.Debugf("Schedule: add '%s' for hook '%s'", scheduleCfg.Crontab, hookName)
			newGlobalHooks.AddHook(globalHook.Name, scheduleCfg)
		}
	}

	// Load active crontabs for module hooks
	newModuleHooks := make(schedule.ScheduleHooksStorage, 0)
	modules := c.moduleManager.GetModuleNamesInOrder()

	for _, moduleName := range modules {
		moduleHooks, _ := c.moduleManager.GetModuleHooksInOrder(moduleName, module_manager.Schedule)
		for _, moduleHookName := range moduleHooks {
			moduleHook, _ := c.moduleManager.GetModuleHook(moduleHookName)
			for _, scheduleCfg := range moduleHook.Config.Schedules {
				_, err := c.scheduleManager.Add(scheduleCfg.Crontab)
				if err != nil {
					rlog.Errorf("Schedule: cannot add '%s' for hook '%s': %s", scheduleCfg.Crontab, moduleHookName, err)
					continue
				}
				rlog.Debugf("Schedule: add '%s' for hook '%s'", scheduleCfg.Crontab, moduleHookName)
				newModuleHooks.AddHook(moduleHook.Name, scheduleCfg)
			}
		}
	}

	// Calculate obsolete crontab strings
	oldCrontabs := map[string]bool{}
	for _, crontab := range c.GlobalHooks.GetCrontabs() {
		oldCrontabs[crontab] = false
	}
	for _, crontab := range c.ModuleHooks.GetCrontabs() {
		oldCrontabs[crontab] = false
	}
	for _, crontab := range newGlobalHooks.GetCrontabs() {
		oldCrontabs[crontab] = true
	}
	for _, crontab := range newModuleHooks.GetCrontabs() {
		oldCrontabs[crontab] = true
	}

	// Stop crontabs that is not in new storages.
	for crontab, isActive := range oldCrontabs {
		if !isActive {
			c.scheduleManager.Remove(crontab)
		}
	}

	c.GlobalHooks = newGlobalHooks
	c.ModuleHooks = newModuleHooks
}

func (c *scheduleHooksController) HandleEvent(crontab string) ([]task.Task, error) {
	res := make([]task.Task, 0)

	// search for global hooks by crontab
	scheduleGlobalHooks := c.GlobalHooks.GetHooksForSchedule(crontab)

	for _, scheduleHook := range scheduleGlobalHooks {
		_, err := c.moduleManager.GetGlobalHook(scheduleHook.HookName)
		if err != nil {
			rlog.Errorf("Possible a bug: global hook '%s' is registered for schedule but not found", scheduleHook.HookName)
			continue
		}
		newTask := task.NewTask(task.GlobalHookRun, scheduleHook.HookName).
			WithBinding(module_manager.Schedule).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: scheduleHook.ConfigName}}).
			WithAllowFailure(scheduleHook.AllowFailure)
		res = append(res, newTask)
	}

	// search for module hooks by crontab
	scheduleModuleHooks := c.ModuleHooks.GetHooksForSchedule(crontab)

	for _, scheduleHook := range scheduleModuleHooks {
		_, err := c.moduleManager.GetModuleHook(scheduleHook.HookName)
		if err != nil {
			rlog.Errorf("Possible a bug: module hook '%s' is registered for schedule but not found", scheduleHook.HookName)
			continue
		}
		newTask := task.NewTask(task.ModuleHookRun, scheduleHook.HookName).
			WithBinding(module_manager.Schedule).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: scheduleHook.ConfigName}}).
			WithAllowFailure(scheduleHook.AllowFailure)
		res = append(res, newTask)
	}

	if len(res) == 0 {
		// NOTE should not happen
		return nil, fmt.Errorf("Possible a bug: schedule event for crontab '%s' received, but no hook is found", crontab)
	}

	return res, nil
}
