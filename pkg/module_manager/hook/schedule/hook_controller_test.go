package schedule

import (
	"testing"

	"github.com/stretchr/testify/assert"

	hook2 "github.com/flant/shell-operator/pkg/hook"

	"github.com/flant/addon-operator/pkg/module_manager"
)


type ScheduleManagerMock struct {
	Added map[string]bool
	Removed map[string]bool
}

func NewScheduleManagerMock() *ScheduleManagerMock {
	return &ScheduleManagerMock{
		Added: make(map[string]bool, 0),
		Removed: make(map[string]bool, 0),
	}
}

func (s *ScheduleManagerMock) Add(crontab string) (string, error) {
	s.Added[crontab] = true
	return crontab, nil
}

func (s *ScheduleManagerMock) Remove(entryId string) error {
	s.Removed[entryId] = true
	return nil
}

func (s *ScheduleManagerMock) Run() {
	return
}

func Test_Schedule_Controller_UpdateHooks(t *testing.T) {
	var sm *ScheduleManagerMock

	var globalHook1 = &module_manager.GlobalHook{
		CommonHook: &module_manager.CommonHook{
			Name: "hook-global",
		},
		Config: &module_manager.GlobalHookConfig{
			HookConfig: hook2.HookConfig{
				Schedules: []hook2.ScheduleConfig{
					{
						Crontab: "*/5 * * * * *",
					},
				},
			},
		},
	}

	var moduleHook1 = &module_manager.ModuleHook{
		CommonHook: &module_manager.CommonHook{
			Name: "hook-module-1",
		},
		Config: &module_manager.ModuleHookConfig{
			HookConfig: hook2.HookConfig{
				Schedules: []hook2.ScheduleConfig{
					{
						Crontab: "*/10 * * * * *",
					},
				},
			},
		},
	}

	var moduleHook2 = &module_manager.ModuleHook{
		CommonHook: &module_manager.CommonHook{
			Name: "hook-module-2",
		},
		Config: &module_manager.ModuleHookConfig{
			HookConfig: hook2.HookConfig{
				Schedules: []hook2.ScheduleConfig{
					{
						Crontab: "*/20 * * * * *",
					},
				},
			},
		},
	}

	var defaultGlobalHooks = map[string]*module_manager.GlobalHook{
		"hook-global": globalHook1,
	}
	var defaultModuleHooks = map[string]map[string]*module_manager.ModuleHook{}

	var globalHooksMock map[string]*module_manager.GlobalHook
	var moduleHooksMock map[string]map[string]*module_manager.ModuleHook
	var mm = module_manager.NewModuleManagerMock(module_manager.ModuleManagerMockFns{
		GetGlobalHook: func(name string) (hook *module_manager.GlobalHook, e error) {
			return globalHooksMock[name], nil
		},
		GetModuleHook: func(name string) (hook *module_manager.ModuleHook, e error) {
			for _, moduleHooks := range moduleHooksMock{
				if hook, ok := moduleHooks[name]; ok {
					return hook, nil
				}
			}
			return nil, nil
		},
		GetGlobalHooksInOrder: func(bindingType module_manager.BindingType) []string {
			res := []string{}
			for k := range globalHooksMock{
				res = append(res, k)
			}
			return res
		},
		GetModuleNamesInOrder: func() []string {
			res := []string{}
			for k := range moduleHooksMock{
				res = append(res, k)
			}
			return res
		},
		GetModuleHooksInOrder: func(moduleName string, bindingType module_manager.BindingType) (strings []string, e error) {
			res := []string{}
			for k := range moduleHooksMock[moduleName]{
				res = append(res, k)
			}
			return res, nil
		},
	})


	var err error
	var ctrl *scheduleHooksController
	tests := []struct{
		name string
		assertion func()
	}{
		{
			"update with global hook",
			func() {
				assert.NoError(t, err)
				assert.Equal(t, true, sm.Added["*/5 * * * * *"])
				assert.Len(t, ctrl.GlobalHooks, 1)
				assert.Len(t, ctrl.GlobalHooks.GetCrontabs(), 1)
				assert.Len(t, ctrl.GlobalHooks.GetHooksForSchedule("*/5 * * * * *"), 1)
				hooks := ctrl.GlobalHooks.GetHooksForSchedule("*/5 * * * * *")
				assert.Equal(t, "hook-global", hooks[0].HookName)
			},
		},
		{
			"module enable",
			func() {
				moduleHooksMock["module-1"] = map[string]*module_manager.ModuleHook{
					"hook-module-1": moduleHook1,
				}
				ctrl.UpdateScheduleHooks()

				assert.Equal(t, true, sm.Added["*/10 * * * * *"])
				assert.Len(t, ctrl.ModuleHooks, 1)
				assert.Len(t, ctrl.ModuleHooks.GetCrontabs(), 1)
				assert.Len(t, ctrl.ModuleHooks.GetHooksForSchedule("*/10 * * * * *"), 1)
				hooks := ctrl.ModuleHooks.GetHooksForSchedule("*/10 * * * * *")
				assert.Equal(t, "hook-module-1", hooks[0].HookName)
			},
		},
		{
			"enable two modules",
			func() {
				moduleHooksMock["module-1"] = map[string]*module_manager.ModuleHook{
					"hook-module-1": moduleHook1,
				}
				moduleHooksMock["module-2"] = map[string]*module_manager.ModuleHook{
					"hook-module-2": moduleHook2,
				}
				ctrl.UpdateScheduleHooks()

				assert.Equal(t, true, sm.Added["*/10 * * * * *"])
				assert.Equal(t, true, sm.Added["*/20 * * * * *"])
				assert.Len(t, ctrl.ModuleHooks, 2)
				assert.Len(t, ctrl.ModuleHooks.GetCrontabs(), 2)
				assert.Len(t, ctrl.ModuleHooks.GetHooksForSchedule("*/20 * * * * *"), 1)
				hooks := ctrl.ModuleHooks.GetHooksForSchedule("*/20 * * * * *")
				assert.Equal(t, "hook-module-2", hooks[0].HookName)
			},
		},
		{
			"disable module",
			func() {
				moduleHooksMock["module-1"] = map[string]*module_manager.ModuleHook{
					"hook-module-1": moduleHook1,
				}
				moduleHooksMock["module-2"] = map[string]*module_manager.ModuleHook{
					"hook-module-2": moduleHook2,
				}
				ctrl.UpdateScheduleHooks()

				delete(moduleHooksMock, "module-2")
				ctrl.UpdateScheduleHooks()

				assert.Equal(t, true, sm.Removed["*/20 * * * * *"])
				assert.Len(t, ctrl.ModuleHooks, 1)
				assert.Len(t, ctrl.ModuleHooks.GetCrontabs(), 1)
				assert.Len(t, ctrl.ModuleHooks.GetHooksForSchedule("*/10 * * * * *"), 1)
				assert.Len(t, ctrl.ModuleHooks.GetHooksForSchedule("*/20 * * * * *"), 0)
				hooks := ctrl.ModuleHooks.GetHooksForSchedule("*/10 * * * * *")
				assert.Equal(t, "hook-module-1", hooks[0].HookName)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sm = NewScheduleManagerMock()

			globalHooksMock = defaultGlobalHooks
			moduleHooksMock = defaultModuleHooks

			ctrl = &scheduleHooksController{}
			ctrl.WithModuleManager(mm)
			ctrl.WithScheduleManager(sm)

			ctrl.UpdateScheduleHooks()

			test.assertion()
		})
	}
}

