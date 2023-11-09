package module_manager

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/shell-operator/pkg/hook"
)

type CommonHook struct {
	hook.Hook

	moduleManager *ModuleManager

	GoHook go_hook.GoHook
}

func (h *CommonHook) WithModuleManager(moduleManager *ModuleManager) {
	h.moduleManager = moduleManager
}

func (h *CommonHook) WithGoHook(gh go_hook.GoHook) {
	h.GoHook = gh
}

func (h *CommonHook) GetName() string {
	return h.Name
}

func (h *CommonHook) GetPath() string {
	return h.Path
}

// GetGoHook implements the Hook interface
func (h *CommonHook) GetGoHook() go_hook.GoHook {
	return h.GoHook
}

// SynchronizationNeeded is true if there is binding with executeHookOnSynchronization.
func (h *CommonHook) SynchronizationNeeded() bool {
	for _, kubeBinding := range h.Config.OnKubernetesEvents {
		if kubeBinding.ExecuteHookOnSynchronization {
			return true
		}
	}
	return false
}

//// ShouldEnableSchedulesOnStartup returns true for Go hooks if EnableSchedulesOnStartup is set.
//// This flag for schedule hooks that start after onStartup hooks.
//func (h *CommonHook) ShouldEnableSchedulesOnStartup() bool {
//	if h.GoHook == nil {
//		return false
//	}
//
//	s := h.GoHook.Config().Settings
//
//	if s != nil && s.EnableSchedulesOnStartup {
//		return true
//	}
//
//	return false
//}
