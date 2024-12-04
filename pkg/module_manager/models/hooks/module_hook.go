package hooks

import (
	"fmt"
	"strings"

	addon_op_types "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/shell-operator/pkg/hook/controller"
	shell_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

// ModuleHook hook which belongs to some module
type ModuleHook struct {
	executableHook
	config *ModuleHookConfig
}

// NewModuleHook build new hook for a module
//
//	ex - some kind of executable hook (GoHook or ShellHook)
func NewModuleHook(ex executableHook) *ModuleHook {
	return &ModuleHook{
		executableHook: ex,
		config:         &ModuleHookConfig{},
	}
}

// GetConfigVersion returns config version
func (mh *ModuleHook) GetConfigVersion() string {
	return mh.config.Version
}

// GetHookConfig returns config for the module hook, it has some difference with global hook
func (mh *ModuleHook) GetHookConfig() *ModuleHookConfig {
	return mh.config
}

// Order returns hook order
func (mh *ModuleHook) Order(binding shell_op_types.BindingType) float64 {
	if mh.config.HasBinding(binding) {
		switch binding {
		case shell_op_types.OnStartup:
			return mh.config.OnStartup.Order
		case addon_op_types.BeforeHelm:
			return mh.config.BeforeHelm.Order
		case addon_op_types.AfterHelm:
			return mh.config.AfterHelm.Order
		case addon_op_types.AfterDeleteHelm:
			return mh.config.AfterDeleteHelm.Order
		}
	}
	return 0.0
}

// InitializeHookConfig initializes the global hook config
// for GoHook config is precompiled, so we just have to fetch it
// for ShellHook, that hook will be run with `--config` flag, returns and parses the config
// for BatchHook, that hook will be run with `hook config` args, returns and parses the config
func (mh *ModuleHook) InitializeHookConfig() (err error) {
	switch hk := mh.executableHook.(type) {
	case *kind.GoHook:
		cfg := hk.GetConfig()

		err := mh.config.LoadAndValidateGoConfig(cfg)
		if err != nil {
			return fmt.Errorf("load and validate go hook config: %w", err)
		}

	case *kind.ShellHook:
		cfg, err := hk.GetConfig()
		if err != nil {
			return fmt.Errorf("get shell hook config: %w", err)
		}

		err = mh.config.LoadAndValidateShellConfig(cfg)
		if err != nil {
			return fmt.Errorf("load and validate shell hook config: %w", err)
		}

	case *kind.BatchHook:
		cfg, err := hk.GetConfig()
		if err != nil {
			return fmt.Errorf("get batch hook config: %w", err)
		}

		err = mh.config.LoadAndValidateBatchConfig(&cfg[hk.ID])
		if err != nil {
			return fmt.Errorf("load and validate batch hook config: %w", err)
		}

	default:
		return fmt.Errorf("unknown hook kind: %T", hk)
	}

	// Make HookController and GetConfigDescription work.
	mh.executableHook.BackportHookConfig(&mh.config.HookConfig)

	return nil
}

// SynchronizationNeeded is true if there is binding with executeHookOnSynchronization.
func (mh *ModuleHook) SynchronizationNeeded() bool {
	for _, kubeBinding := range mh.config.OnKubernetesEvents {
		if kubeBinding.ExecuteHookOnSynchronization {
			return true
		}
	}
	return false
}

// WithHookController set HookController for shell-operator
func (mh *ModuleHook) WithHookController(ctrl *controller.HookController) {
	mh.executableHook.WithHookController(ctrl)
}

// WithTmpDir proxy method to set temp directory for the executable hook
func (mh *ModuleHook) WithTmpDir(tmpDir string) {
	mh.executableHook.WithTmpDir(tmpDir)
}

// ApplyBindingActions some kind of runtime monitor bindings update
func (mh *ModuleHook) ApplyBindingActions(bindingActions []gohook.BindingAction) error {
	for _, action := range bindingActions {
		bindingIdx := -1
		for i, binding := range mh.config.OnKubernetesEvents {
			if binding.BindingName == action.Name {
				bindingIdx = i
			}
		}
		if bindingIdx == -1 {
			continue
		}

		monitorCfg := mh.config.OnKubernetesEvents[bindingIdx].Monitor
		switch strings.ToLower(action.Action) {
		case "disable":
			// Empty kind - "null" monitor.
			monitorCfg.Kind = ""
			monitorCfg.ApiVersion = ""
			monitorCfg.Metadata.MetricLabels["kind"] = ""
		case "updatekind":
			monitorCfg.Kind = action.Kind
			monitorCfg.ApiVersion = action.ApiVersion
			monitorCfg.Metadata.MetricLabels["kind"] = action.Kind
		default:
			continue
		}

		// Recreate monitor. Synchronization phase is ignored, kubernetes events are allowed.
		err := mh.GetHookController().UpdateMonitor(monitorCfg.Metadata.MonitorId, action.Kind, action.ApiVersion)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetConfigDescription returns config description for debugging/logging
func (mh *ModuleHook) GetConfigDescription() string {
	bd := make([]string, 0, 1)

	if mh.config.BeforeHelm != nil {
		bd = append(bd, fmt.Sprintf("beforeHelm:%d", int64(mh.config.BeforeHelm.Order)))
	}
	if mh.config.AfterHelm != nil {
		bd = append(bd, fmt.Sprintf("afterHelm:%d", int64(mh.config.AfterHelm.Order)))
	}
	if mh.config.AfterDeleteHelm != nil {
		bd = append(bd, fmt.Sprintf("afterDeleteHelm:%d", int64(mh.config.AfterDeleteHelm.Order)))
	}
	bd = append(bd, mh.executableHook.GetHookConfigDescription())

	return strings.Join(bd, ", ")
}

// GetGoHookInputSettings proxy method to extract GoHook config settings
func (mh *ModuleHook) GetGoHookInputSettings() *gohook.HookConfigSettings {
	if mh.GetKind() != kind.HookKindGo {
		return nil
	}

	gohook := mh.executableHook.(*kind.GoHook)
	return gohook.GetConfig().Settings
}
