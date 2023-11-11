package hooks

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/flant/addon-operator/pkg/utils"

	types2 "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/shell-operator/pkg/hook/types"
)

type GlobalHook struct {
	executableHook
	config *GlobalHookConfig

	dc *HookExecutionDependencyContainer
}

func NewGlobalHook(ex executableHook, dc *HookExecutionDependencyContainer) *GlobalHook {
	return &GlobalHook{
		executableHook: ex,
		config:         &GlobalHookConfig{},

		dc: dc,
	}
}

func (h *GlobalHook) InitializeHookConfig() (err error) {
	switch hk := h.executableHook.(type) {
	case *kind.GoHook:
		cfg := hk.GetConfig()
		err := h.config.LoadAndValidateGoConfig(cfg)
		if err != nil {
			return err
		}

	case *kind.ShellHook:
		cfg, err := hk.GetConfig()
		if err != nil {
			return err
		}
		err = h.config.LoadAndValidateShellConfig(cfg)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown hook kind: %s", reflect.TypeOf(hk))
	}

	// Make HookController and GetConfigDescription work.
	h.executableHook.BackportHookConfig(&h.config.HookConfig)

	return nil
}

func (h *GlobalHook) GetHookConfig() *GlobalHookConfig {
	return h.config
}

// import (
//
//	"fmt"
//	"path/filepath"
//	"strings"
//
//	uuid "github.com/gofrs/uuid/v5"
//	"github.com/hashicorp/go-multierror"
//	log "github.com/sirupsen/logrus"
//
//	. "github.com/flant/addon-operator/pkg/hook/types"
//	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
//	"github.com/flant/addon-operator/pkg/utils"
//	"github.com/flant/shell-operator/pkg/hook"
//	. "github.com/flant/shell-operator/pkg/hook/binding_context"
//	. "github.com/flant/shell-operator/pkg/hook/types"
//
// )
//
//	type GlobalHook struct {
//		*CommonHook
//		Config *GlobalHookConfig
//	}
//
//	func NewGlobalHook(name, path string) *GlobalHook {
//		res := &GlobalHook{
//			CommonHook: &CommonHook{},
//			Config:     &GlobalHookConfig{},
//		}
//		res.Name = name
//		res.Path = path
//		return res
//	}
//
//	func (h *GlobalHook) WithConfig(configOutput []byte) (err error) {
//		err = h.Config.LoadAndValidate(configOutput)
//		if err != nil {
//			return fmt.Errorf("load global hook '%s' config: %s\nhook --config output: %s", h.Name, err.Error(), configOutput)
//		}
//		// Make HookController and GetConfigDescription work.
//		h.Hook.Config = &h.Config.HookConfig
//		h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)
//
//		return nil
//	}
//
//	func (h *GlobalHook) WithGoConfig(config *go_hook.HookConfig) (err error) {
//		h.Config, err = newGlobalHookConfigFromGoConfig(config)
//		if err != nil {
//			return err
//		}
//
//		// Make HookController and GetConfigDescription work.
//		h.Hook.Config = &h.Config.HookConfig
//		h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)
//		return nil
//	}

//func (h *GlobalHook) GetName() string {
//	return h.hook.GetName()
//}

//func (h *GlobalHook) WithHookController(ctrl controller.HookController) {
//	h.hook.WithHookController(ctrl)
//}
//
//func (h *GlobalHook) WithTmpDir(tmpDir string) {
//	h.hook.WithTmpDir(tmpDir)
//}

//	func (h *GlobalHook) GetPath() string {
//		return h.hook.GetPath()
//	}
func (h *GlobalHook) GetConfigDescription() string {
	msgs := make([]string, 0)
	if h.config.BeforeAll != nil {
		msgs = append(msgs, fmt.Sprintf("beforeAll:%d", int64(h.config.BeforeAll.Order)))
	}
	if h.config.AfterAll != nil {
		msgs = append(msgs, fmt.Sprintf("afterAll:%d", int64(h.config.AfterAll.Order)))
	}
	msgs = append(msgs, h.executableHook.GetHookConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (h *GlobalHook) Order(binding types.BindingType) float64 {
	if h.config.HasBinding(binding) {
		switch binding {
		case types2.BeforeAll:
			return h.config.BeforeAll.Order
		case types2.AfterAll:
			return h.config.AfterAll.Order
		case types.OnStartup:
			return h.config.OnStartup.Order
		}
	}
	return 0.0
}

//func (h *GlobalHook) GetHookController() controller.HookController {
//	return h.hook.GetHookController()
//}

// Apply patches to enabled modules
func (h *GlobalHook) ApplyEnabledPatches(valuesPatch utils.ValuesPatch) error {
	enabledPatch := utils.EnabledFromValuesPatch(valuesPatch)
	if len(enabledPatch.Operations) != 0 {

		for _, p := range enabledPatch.Operations {
			fmt.Printf("Patch: %+v\n", p)
		}
		panic("enable patch")
		//err := h.moduleManager.ApplyEnabledPatch(enabledPatch)
		//if err != nil {
		//	return err
		//}
	}
	return nil
}

func (h *GlobalHook) GetConfigVersion() string {
	return h.config.Version
}

//
//func (h *GlobalHook) Run(bindingType types.BindingType, bindingContext []binding_context.BindingContext, logLabels map[string]string) error {
//	// Convert bindingContext for version
//	// versionedContextList := ConvertBindingContextList(h.Config.Version, bindingContext)
//	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
//
//	for _, info := range h.GetHookController().SnapshotsInfo() {
//		logEntry.Debugf("snapshot info: %s", info)
//	}
//
//	hookResult, err := h.executableHook.Execute(h.config.Version, bindingContext, "global", configValues, values, logLabels)
//	if hookResult != nil && hookResult.Usage != nil {
//		metricLabels := map[string]string{
//			"hook":       h.GetName(),
//			"binding":    string(bindingType),
//			"queue":      logLabels["queue"],
//			"activation": logLabels["event.type"],
//		}
//		// usage metrics
//		h.dc.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels, nil)
//		h.dc.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels, nil)
//		h.dc.MetricStorage.GaugeSet("{PREFIX}global_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
//	}
//	if err != nil {
//		return fmt.Errorf("global hook '%s' failed: %s", h.GetName(), err)
//	}
//
//	// Apply metric operations
//	err = h.dc.HookMetricsStorage.SendBatch(hookResult.Metrics, map[string]string{
//		"hook": h.GetName(),
//	})
//	if err != nil {
//		return err
//	}
//
//	if len(hookResult.ObjectPatcherOperations) > 0 {
//		err = h.dc.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
//		if err != nil {
//			return err
//		}
//	}
//
//	configValuesPatch, has := hookResult.Patches[utils.ConfigMapPatch]
//	if has && configValuesPatch != nil {
//		//preparedConfigValues := utils.MergeValues(
//		//	utils.Values{"global": map[string]interface{}{}},
//		//	h.moduleManager.GetKubeConfigValues("global"),
//		//)
//
//		currentConfigValuesForPatch := utils.Values{"global": h.},
//
//		// Apply patch to get intermediate updated values.
//		configValuesPatchResult, err := h.handleGlobalValuesPatch(currentConfigValuesForPatch, *configValuesPatch)
//		if err != nil {
//			return fmt.Errorf("global hook '%s': kube config global values update error: %s", h.GetName(), err)
//		}
//
//		if configValuesPatchResult != nil && configValuesPatchResult.ValuesChanged {
//			logEntry.Debugf("Global hook '%s': validate global config values before update", h.GetName())
//			// Validate merged static and new values.
//			mergedValues := h.moduleManager.GlobalStaticAndNewValues(configValuesPatchResult.Values)
//			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalConfigValues(mergedValues)
//			if validationErr != nil {
//				return multierror.Append(
//					fmt.Errorf("cannot apply config values patch for global values"),
//					validationErr,
//				)
//			}
//
//			err := h.dc.KubeConfigManager.SaveConfigValues(utils.GlobalValuesKey, configValuesPatchResult.Values)
//			if err != nil {
//				logEntry.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.GetName(), h.moduleManager.GetKubeConfigValues("global").DebugString())
//				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.GetName(), err)
//			}
//
//			h.moduleManager.UpdateGlobalConfigValues(configValuesPatchResult.Values)
//
//			logEntry.Debugf("Global hook '%s': kube config global values updated", h.GetName())
//			logEntry.Debugf("New kube config global values:\n%s\n", h.moduleManager.GetKubeConfigValues("global").DebugString())
//		}
//		// Apply patches for *Enabled keys.
//		err = h.applyEnabledPatches(*configValuesPatch)
//		if err != nil {
//			return fmt.Errorf("apply enabled patches from global config patch: %v", err)
//		}
//	}
//
//	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
//	if has && valuesPatch != nil {
//		globalValues, err := h.moduleManager.GlobalValues()
//		if err != nil {
//			return fmt.Errorf("global hook '%s': global values before patch apply: %s", h.Name, err)
//		}
//
//		// Apply patch to get intermediate updated values.
//		valuesPatchResult, err := h.handleGlobalValuesPatch(globalValues, *valuesPatch)
//		if err != nil {
//			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.Name, err)
//		}
//
//		// MemoryValuesPatch from global hook can contains patches for *Enabled keys
//		// and no patches for 'global' section — valuesPatchResult will be nil in this case.
//		if valuesPatchResult != nil && valuesPatchResult.ValuesChanged {
//			logEntry.Debugf("Global hook '%s': validate global values before update", h.Name)
//			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalValues(valuesPatchResult.Values)
//			if validationErr != nil {
//				return multierror.Append(
//					fmt.Errorf("cannot apply values patch for global values"),
//					validationErr,
//				)
//			}
//
//			h.moduleManager.UpdateGlobalDynamicValuesPatches(valuesPatchResult.ValuesPatch)
//			newGlobalValues, err := h.moduleManager.GlobalValues()
//			if err != nil {
//				return fmt.Errorf("global hook '%s': global values after patch apply: %s", h.Name, err)
//			}
//			logEntry.Debugf("Global hook '%s': kube global values updated", h.Name)
//			logEntry.Debugf("New global values:\n%s", newGlobalValues.DebugString())
//		}
//		// Apply patches for *Enabled keys.
//		err = h.applyEnabledPatches(*valuesPatch)
//		if err != nil {
//			return fmt.Errorf("apply enabled patches from global values patch: %v", err)
//		}
//	}
//
//	return nil
//}

// SynchronizationNeeded is true if there is binding with executeHookOnSynchronization.
func (h *GlobalHook) SynchronizationNeeded() bool {
	for _, kubeBinding := range h.config.OnKubernetesEvents {
		if kubeBinding.ExecuteHookOnSynchronization {
			return true
		}
	}
	return false
}

//// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
//func (h *GlobalHook) PrepareTmpFilesForHookRun(bindingContext []byte) (tmpFiles map[string]string, err error) {
//	tmpFiles = make(map[string]string)
//
//	tmpFiles["CONFIG_VALUES_PATH"], err = h.prepareConfigValuesJsonFile()
//	if err != nil {
//		return
//	}
//
//	tmpFiles["VALUES_PATH"], err = h.prepareValuesJsonFile()
//	if err != nil {
//		return
//	}
//
//	tmpFiles["BINDING_CONTEXT_PATH"], err = h.prepareBindingContextJsonFile(bindingContext)
//	if err != nil {
//		return
//	}
//
//	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err = h.prepareConfigValuesJsonPatchFile()
//	if err != nil {
//		return
//	}
//
//	tmpFiles["VALUES_JSON_PATCH_PATH"], err = h.prepareValuesJsonPatchFile()
//	if err != nil {
//		return
//	}
//
//	tmpFiles["METRICS_PATH"], err = h.prepareMetricsFile()
//	if err != nil {
//		return
//	}
//
//	tmpFiles["KUBERNETES_PATCH_PATH"], err = h.prepareKubernetesPatchFile()
//	if err != nil {
//		return
//	}
//
//	return
//}
//
//func (h *GlobalHook) GetConfigValues() utils.Values {
//	return h.moduleManager.GlobalConfigValues()
//}
//
//// CONFIG_VALUES_PATH
//func (h *GlobalHook) prepareConfigValuesJsonFile() (string, error) {
//	configValues := h.GetConfigValues()
//	data, err := configValues.JsonBytes()
//	if err != nil {
//		return "", err
//	}
//
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-config-values-%s.json", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	err = utils.DumpData(path, data)
//	if err != nil {
//		return "", err
//	}
//
//	log.Debugf("Prepared global hook %s config values:\n%s", h.Name, configValues.DebugString())
//
//	return path, nil
//}
//
//func (h *GlobalHook) GetValues() (utils.Values, error) {
//	return h.moduleManager.GlobalValues()
//}
//
//// VALUES_PATH
//func (h *GlobalHook) prepareValuesJsonFile() (filePath string, err error) {
//	values, err := h.GetValues()
//	if err != nil {
//		return "", nil
//	}
//	data, err := values.JsonBytes()
//	if err != nil {
//		return "", err
//	}
//
//	filePath = filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-values-%s.json", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	err = utils.DumpData(filePath, data)
//	if err != nil {
//		return "", err
//	}
//
//	log.Debugf("Prepared global hook %s values:\n%s", h.Name, values.DebugString())
//
//	return filePath, nil
//}
//
//// BINDING_CONTEXT_PATH
//func (h *GlobalHook) prepareBindingContextJsonFile(bindingContext []byte) (string, error) {
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-binding-context-%s.json", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	err := utils.DumpData(path, bindingContext)
//	if err != nil {
//		return "", err
//	}
//
//	return path, nil
//}
//
//// CONFIG_VALUES_JSON_PATCH_PATH
//func (h *GlobalHook) prepareConfigValuesJsonPatchFile() (string, error) {
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-config-values-%s.json-patch", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	if err := utils.CreateEmptyWritableFile(path); err != nil {
//		return "", err
//	}
//	return path, nil
//}
//
//// VALUES_JSON_PATCH_PATH
//func (h *GlobalHook) prepareValuesJsonPatchFile() (string, error) {
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-values-%s.json-patch", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	if err := utils.CreateEmptyWritableFile(path); err != nil {
//		return "", err
//	}
//	return path, nil
//}
//
//// METRICS_PATH
//func (h *GlobalHook) prepareMetricsFile() (string, error) {
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-metrics-%s.json", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	if err := utils.CreateEmptyWritableFile(path); err != nil {
//		return "", err
//	}
//	return path, nil
//}
//
//// KUBERNETES PATCH PATH
//func (h *GlobalHook) prepareKubernetesPatchFile() (string, error) {
//	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s-object-patch-%s", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
//	if err := utils.CreateEmptyWritableFile(path); err != nil {
//		return "", err
//	}
//	return path, nil
//}
