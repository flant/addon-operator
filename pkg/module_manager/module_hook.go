package module_manager

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	uuid "gopkg.in/satori/go.uuid.v1"

	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
)

type ModuleHook struct {
	*CommonHook
	Module *Module
	Config *ModuleHookConfig
}

var _ Hook = &ModuleHook{}

func NewModuleHook(name, path string) *ModuleHook {
	res := &ModuleHook{
		CommonHook: &CommonHook{},
		Config:     &ModuleHookConfig{},
	}
	res.Name = name
	res.Path = path
	return res
}

func (h *ModuleHook) WithModule(module *Module) {
	h.Module = module
}

func (h *ModuleHook) WithConfig(configOutput []byte) (err error) {
	err = h.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load module hook '%s' config: %s\nhook --config output: %s", h.Name, err.Error(), configOutput)
	}
	// Make HookController and GetConfigDescription work.
	h.Hook.Config = &h.Config.HookConfig
	h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)

	return nil
}

func (h *ModuleHook) WithGoConfig(config *go_hook.HookConfig) (err error) {
	h.Config, err = NewModuleHookConfigFromGoConfig(config)
	if err != nil {
		return err
	}

	// Make HookController and GetConfigDescription work.
	h.Hook.Config = &h.Config.HookConfig
	h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)
	return nil
}

func (h *ModuleHook) GetConfigDescription() string {
	msgs := []string{}
	if h.Config.BeforeHelm != nil {
		msgs = append(msgs, fmt.Sprintf("beforeHelm:%d", int64(h.Config.BeforeHelm.Order)))
	}
	if h.Config.AfterHelm != nil {
		msgs = append(msgs, fmt.Sprintf("afterHelm:%d", int64(h.Config.AfterHelm.Order)))
	}
	if h.Config.AfterDeleteHelm != nil {
		msgs = append(msgs, fmt.Sprintf("afterDeleteHelm:%d", int64(h.Config.AfterDeleteHelm.Order)))
	}
	msgs = append(msgs, h.Hook.GetConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (h *ModuleHook) Order(binding BindingType) float64 {
	if h.Config.HasBinding(binding) {
		switch binding {
		case OnStartup:
			return h.Config.OnStartup.Order
		case BeforeHelm:
			return h.Config.BeforeHelm.Order
		case AfterHelm:
			return h.Config.AfterHelm.Order
		case AfterDeleteHelm:
			return h.Config.AfterDeleteHelm.Order
		}
	}
	return 0.0
}

type moduleValuesMergeResult struct {
	// global values with root ModuleValuesKey key
	Values          utils.Values
	ModuleValuesKey string
	ValuesPatch     utils.ValuesPatch
	ValuesChanged   bool
}

func (h *ModuleHook) handleModuleValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*moduleValuesMergeResult, error) {
	moduleValuesKey := h.Module.ValuesKey()

	if err := utils.ValidateHookValuesPatch(valuesPatch, moduleValuesKey); err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", h.Module.Name, err)
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.FastApplyValuesPatch(currentValues, valuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", h.Module.Name, err)
	}

	result := &moduleValuesMergeResult{
		ModuleValuesKey: moduleValuesKey,
		Values:          utils.Values{moduleValuesKey: make(map[string]interface{})},
		ValuesChanged:   valuesChanged,
		ValuesPatch:     valuesPatch,
	}

	if newValues.HasKey(moduleValuesKey) {
		_, ok := newValues[moduleValuesKey].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map at key '%s', got:\n%s", result.ModuleValuesKey, newValues.SectionByKey(moduleValuesKey).DebugString())
		}
		result.Values = newValues.SectionByKey(moduleValuesKey)
	}

	return result, nil
}

func (h *ModuleHook) Run(bindingType BindingType, context []BindingContext, logLabels map[string]string, metricLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"hook":      h.Name,
		"hook.type": "module",
		"binding":   string(bindingType),
	})

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	logStartLevel := log.InfoLevel
	// Use Debug when run as a separate task for Kubernetes or Schedule hooks, as task start is already logged.
	// TODO log this message by callers.
	if bindingType == OnKubernetesEvent || bindingType == Schedule {
		logStartLevel = log.DebugLevel
	}
	logEntry.Logf(logStartLevel, "Module hook start %s/%s", h.Module.Name, h.Name)

	for _, info := range h.HookController.SnapshotsInfo() {
		logEntry.Debugf("snapshot info: %s", info)
	}

	// Convert context for version
	// versionedContextList := ConvertBindingContextList(h.Config.Version, context)

	moduleHookExecutor := NewHookExecutor(h, context, h.Config.Version, h.moduleManager.KubeObjectPatcher)
	moduleHookExecutor.WithLogLabels(logLabels)
	moduleHookExecutor.WithHelm(h.moduleManager.helm)
	hookResult, err := moduleHookExecutor.Run()
	if hookResult != nil && hookResult.Usage != nil {
		// usage metrics
		h.moduleManager.metricStorage.HistogramObserve("{PREFIX}module_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels, nil)
		h.moduleManager.metricStorage.HistogramObserve("{PREFIX}module_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels, nil)
		h.moduleManager.metricStorage.GaugeSet("{PREFIX}module_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		return fmt.Errorf("module hook '%s' failed: %s", h.Name, err)
	}

	moduleName := h.Module.Name

	if len(hookResult.ObjectPatcherOperations) > 0 {
		err = h.moduleManager.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
		if err != nil {
			return err
		}
	}

	// Apply metric operations
	err = h.moduleManager.hookMetricStorage.SendBatch(hookResult.Metrics, map[string]string{
		"hook":   h.Name,
		"module": moduleName,
	})
	if err != nil {
		return err
	}

	// Apply binding actions. (Only Go hook for now).
	if h.GoHook != nil {
		err = h.moduleManager.ApplyBindingActions(h, hookResult.BindingActions)
		if err != nil {
			return err
		}
	}

	configValuesPatch, has := hookResult.Patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil {
		configValues := h.Module.ConfigValues()

		// Apply patch to get intermediate updated values.
		configValuesPatchResult, err := h.handleModuleValuesPatch(configValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': kube module config values update error: %s", h.Name, err)
		}

		if configValuesPatchResult.ValuesChanged {
			logEntry.Debugf("Module hook '%s': validate module config values before update", h.Name)
			// Validate merged static and new values.
			mergedValues := h.Module.StaticAndNewValues(configValuesPatchResult.Values)
			validationErr := h.moduleManager.ValuesValidator.ValidateModuleConfigValues(h.Module.ValuesKey(), mergedValues)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply config values patch for module values"),
					validationErr,
				)
			}

			err := h.moduleManager.kubeConfigManager.SaveModuleConfigValues(moduleName, configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debugf("Module hook '%s' kube module config values stay unchanged:\n%s", h.Name, h.moduleManager.kubeModulesConfigValues[moduleName].DebugString())
				return fmt.Errorf("module hook '%s': set kube module config failed: %s", h.Name, err)
			}

			h.moduleManager.UpdateModuleConfigValues(moduleName, configValuesPatchResult.Values)
			logEntry.Debugf("Module hook '%s': kube module '%s' config values updated:\n%s", h.Name, moduleName, h.moduleManager.kubeModulesConfigValues[moduleName].DebugString())
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		currentValues, err := h.Module.Values()
		if err != nil {
			return fmt.Errorf("get module values before values patch: %s", err)
		}

		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := h.handleModuleValuesPatch(currentValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': dynamic module values update error: %s", h.Name, err)
		}
		if valuesPatchResult.ValuesChanged {
			logEntry.Debugf("Module hook '%s': validate module values before update", h.Name)
			// Validate schema for updated module values
			validationErr := h.moduleManager.ValuesValidator.ValidateModuleValues(h.Module.ValuesKey(), valuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply values patch for module values"),
					validationErr,
				)
			}

			// Save patch set if everything is ok.
			h.moduleManager.UpdateModuleDynamicValuesPatches(moduleName, valuesPatchResult.ValuesPatch)
			newValues, err := h.Module.Values()
			if err != nil {
				return fmt.Errorf("get module values after values patch: %s", err)
			}
			logEntry.Debugf("Module hook '%s': dynamic module '%s' values updated:\n%s", h.Name, moduleName, newValues.DebugString())
		}
	}

	logEntry.Debugf("Module hook success %s/%s", h.Module.Name, h.Name)

	return nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *ModuleHook) PrepareTmpFilesForHookRun(bindingContext []byte) (tmpFiles map[string]string, err error) {
	tmpFiles = make(map[string]string)

	tmpFiles["CONFIG_VALUES_PATH"], err = h.prepareConfigValuesJsonFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_PATH"], err = h.prepareValuesJsonFile()
	if err != nil {
		return
	}

	tmpFiles["BINDING_CONTEXT_PATH"], err = h.prepareBindingContextJsonFile(bindingContext)
	if err != nil {
		return
	}

	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err = h.prepareConfigValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_JSON_PATCH_PATH"], err = h.prepareValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["METRICS_PATH"], err = h.prepareMetricsFile()
	if err != nil {
		return
	}

	tmpFiles["KUBERNETES_PATCH_PATH"], err = h.prepareKubernetesPatchFile()
	if err != nil {
		return
	}

	return
}

func (h *ModuleHook) GetConfigValues() utils.Values {
	return h.Module.ConfigValues()
}

func (h *ModuleHook) prepareValuesJsonFile() (string, error) {
	return h.Module.prepareValuesJsonFile()
}

func (h *ModuleHook) GetValues() (utils.Values, error) {
	return h.Module.Values()
}

func (h *ModuleHook) prepareConfigValuesJsonFile() (string, error) {
	return h.Module.prepareConfigValuesJsonFile()
}

// BINDING_CONTEXT_PATH
func (h *ModuleHook) prepareBindingContextJsonFile(bindingContext []byte) (string, error) {
	// data := utils.MustDump(utils.DumpValuesJson(context))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-%s-binding-context-%s.json", h.Module.SafeName(), h.SafeName(), uuid.NewV4().String()))
	err := dumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	// FIXME too much information because of snapshots
	// log.Debugf("Prepared module %s hook %s binding context:\n%s", h.Module.SafeName(), h.Name, string(bindingContext))

	return path, nil
}

// CONFIG_VALUES_JSON_PATCH_PATH
func (h *ModuleHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-config-values-%s.json-patch", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// VALUES_JSON_PATCH_PATH
func (h *ModuleHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-values-%s.json-patch", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// METRICS_PATH
func (h *ModuleHook) prepareMetricsFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-metrics-%s.json", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// KUBERNETES PATCH PATH
func (h *ModuleHook) prepareKubernetesPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s-object-patch-%s", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}
