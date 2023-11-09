package hooks

import (
	"context"
	"fmt"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/binding_context"
	"reflect"
	"strings"

	"github.com/flant/shell-operator/pkg/hook/config"

	types2 "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/shell-operator/pkg/hook/controller"
	"github.com/flant/shell-operator/pkg/hook/types"
)

type executableHook interface {
	GetName() string
	GetPath() string

	Execute(configVersion string, bContext []binding_context.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (result *kind.HookResult, err error)
	RateLimitWait(ctx context.Context) error

	WithHookController(ctrl controller.HookController)
	GetHookController() controller.HookController
	WithTmpDir(tmpDir string)

	GetKind() kind.HookKind

	BackportHookConfig(cfg *config.HookConfig)
	GetHookConfigDescription() string
}

type ModuleHook struct {
	executableHook
	config *ModuleHookConfig
}

func NewModuleHook(ex executableHook) *ModuleHook {
	return &ModuleHook{
		executableHook: ex,
		config:         &ModuleHookConfig{},
	}
}

func (mh *ModuleHook) GetConfigVersion() string {
	return mh.config.Version
}

func (mh *ModuleHook) GetHookConfig() *ModuleHookConfig {
	return mh.config
}

func (mh *ModuleHook) Order(binding types.BindingType) float64 {
	if mh.config.HasBinding(binding) {
		switch binding {
		case types.OnStartup:
			return mh.config.OnStartup.Order
		case types2.BeforeHelm:
			return mh.config.BeforeHelm.Order
		case types2.AfterHelm:
			return mh.config.AfterHelm.Order
		case types2.AfterDeleteHelm:
			return mh.config.AfterDeleteHelm.Order
		}
	}
	return 0.0
}

func (mh *ModuleHook) InitializeHookConfig() (err error) {
	switch hk := mh.executableHook.(type) {
	case *kind.GoHook:
		cfg := hk.GetConfig()
		err := mh.config.LoadAndValidateGoConfig(cfg)
		if err != nil {
			return err
		}

	case *kind.ShellHook:
		cfg, err := hk.GetConfig()
		if err != nil {
			return err
		}
		err = mh.config.LoadAndValidateShellConfig(cfg)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown hook kind: %s", reflect.TypeOf(hk))
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

func (mh *ModuleHook) WithHookController(ctrl controller.HookController) {
	mh.executableHook.WithHookController(ctrl)
}

func (mh *ModuleHook) WithTmpDir(tmpDir string) {
	mh.executableHook.WithTmpDir(tmpDir)
}

func (mh *ModuleHook) ApplyBindingActions(bindingActions []go_hook.BindingAction) error {
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

func (mh *ModuleHook) GetConfigDescription() string {
	bd := strings.Builder{}

	if mh.config.BeforeHelm != nil {
		bd.WriteString(fmt.Sprintf("beforeHelm:%d", int64(mh.config.BeforeHelm.Order)))
	}
	if mh.config.AfterHelm != nil {
		bd.WriteString(", " + fmt.Sprintf("afterHelm:%d", int64(mh.config.AfterHelm.Order)))
	}
	if mh.config.AfterDeleteHelm != nil {
		bd.WriteString(", " + fmt.Sprintf("afterDeleteHelm:%d", int64(mh.config.AfterDeleteHelm.Order)))
	}
	bd.WriteString(", " + mh.executableHook.GetHookConfigDescription())

	return bd.String()
}
