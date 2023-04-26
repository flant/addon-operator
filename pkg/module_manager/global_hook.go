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

type GlobalHook struct {
	*CommonHook
	Config *GlobalHookConfig
}

var _ Hook = &GlobalHook{}

func NewGlobalHook(name, path string) *GlobalHook {
	res := &GlobalHook{
		CommonHook: &CommonHook{},
		Config:     &GlobalHookConfig{},
	}
	res.Name = name
	res.Path = path
	return res
}

func (h *GlobalHook) WithConfig(configOutput []byte) (err error) {
	err = h.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load global hook '%s' config: %s\nhook --config output: %s", h.Name, err.Error(), configOutput)
	}
	// Make HookController and GetConfigDescription work.
	h.Hook.Config = &h.Config.HookConfig
	h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)

	return nil
}

func (h *GlobalHook) WithGoConfig(config *go_hook.HookConfig) (err error) {
	h.Config, err = NewGlobalHookConfigFromGoConfig(config)
	if err != nil {
		return err
	}

	// Make HookController and GetConfigDescription work.
	h.Hook.Config = &h.Config.HookConfig
	h.Hook.RateLimiter = hook.CreateRateLimiter(h.Hook.Config)
	return nil
}

func (h *GlobalHook) GetConfigDescription() string {
	msgs := []string{}
	if h.Config.BeforeAll != nil {
		msgs = append(msgs, fmt.Sprintf("beforeAll:%d", int64(h.Config.BeforeAll.Order)))
	}
	if h.Config.AfterAll != nil {
		msgs = append(msgs, fmt.Sprintf("afterAll:%d", int64(h.Config.AfterAll.Order)))
	}
	msgs = append(msgs, h.Hook.GetConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (h *GlobalHook) Order(binding BindingType) float64 {
	if h.Config.HasBinding(binding) {
		switch binding {
		case BeforeAll:
			return h.Config.BeforeAll.Order
		case AfterAll:
			return h.Config.AfterAll.Order
		case OnStartup:
			return h.Config.OnStartup.Order
		}
	}
	return 0.0
}

type globalValuesPatchResult struct {
	// Global values with the root "global" key.
	Values utils.Values
	// Original values patch argument.
	ValuesPatch utils.ValuesPatch
	// Whether values changed after applying patch.
	ValuesChanged bool
}

// handleGlobalValuesPatch do simple checks of patches and apply them to passed Values.
func (h *GlobalHook) handleGlobalValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*globalValuesPatchResult, error) {
	if err := utils.ValidateHookValuesPatch(valuesPatch, utils.GlobalValuesKey); err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	// Get patches for global section
	globalValuesPatch := utils.FilterValuesPatch(valuesPatch, utils.GlobalValuesKey)
	if len(globalValuesPatch.Operations) == 0 {
		// No patches for 'global' section
		return nil, nil
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.ApplyValuesPatch(currentValues, globalValuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	result := &globalValuesPatchResult{
		Values:        utils.Values{utils.GlobalValuesKey: make(map[string]interface{})},
		ValuesChanged: valuesChanged,
		ValuesPatch:   globalValuesPatch,
	}

	if newValues.HasGlobal() {
		_, ok := newValues[utils.GlobalValuesKey].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map at key '%s', got:\n%s", utils.GlobalValuesKey, newValues.Global().DebugString())
		}

		result.Values = newValues.Global()
	}

	return result, nil
}

// Apply patches to enabled modules
func (h *GlobalHook) applyEnabledPatches(valuesPatch utils.ValuesPatch) error {
	enabledPatch := utils.EnabledFromValuesPatch(valuesPatch)
	if len(enabledPatch.Operations) != 0 {
		err := h.moduleManager.ApplyEnabledPatch(enabledPatch)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *GlobalHook) Run(bindingType BindingType, bindingContext []BindingContext, logLabels map[string]string) error {
	// Convert bindingContext for version
	// versionedContextList := ConvertBindingContextList(h.Config.Version, bindingContext)
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	for _, info := range h.HookController.SnapshotsInfo() {
		logEntry.Debugf("snapshot info: %s", info)
	}

	globalHookExecutor := NewHookExecutor(h, bindingContext, h.Config.Version, h.moduleManager.dependencies.KubeObjectPatcher)
	globalHookExecutor.WithLogLabels(logLabels)
	globalHookExecutor.WithHelm(h.moduleManager.dependencies.Helm)
	hookResult, err := globalHookExecutor.Run()
	if hookResult != nil && hookResult.Usage != nil {
		metricLabels := map[string]string{
			"hook":       h.Name,
			"binding":    string(bindingType),
			"queue":      logLabels["queue"],
			"activation": logLabels["event.type"],
		}
		// usage metrics
		h.moduleManager.dependencies.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels, nil)
		h.moduleManager.dependencies.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels, nil)
		h.moduleManager.dependencies.MetricStorage.GaugeSet("{PREFIX}global_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		return fmt.Errorf("global hook '%s' failed: %s", h.Name, err)
	}

	// Apply metric operations
	err = h.moduleManager.dependencies.HookMetricStorage.SendBatch(hookResult.Metrics, map[string]string{
		"hook": h.Name,
	})
	if err != nil {
		return err
	}

	if len(hookResult.ObjectPatcherOperations) > 0 {
		err = h.moduleManager.dependencies.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
		if err != nil {
			return err
		}
	}

	configValuesPatch, has := hookResult.Patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil {
		preparedConfigValues := utils.MergeValues(
			utils.Values{"global": map[string]interface{}{}},
			h.moduleManager.kubeGlobalConfigValues,
		)

		// Apply patch to get intermediate updated values.
		configValuesPatchResult, err := h.handleGlobalValuesPatch(preparedConfigValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': kube config global values update error: %s", h.Name, err)
		}

		if configValuesPatchResult != nil && configValuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global config values before update", h.Name)
			// Validate merged static and new values.
			mergedValues := h.moduleManager.GlobalStaticAndNewValues(configValuesPatchResult.Values)
			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalConfigValues(mergedValues)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply config values patch for global values"),
					validationErr,
				)
			}

			err := h.moduleManager.dependencies.KubeConfigManager.SaveGlobalConfigValues(configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.Name, h.moduleManager.kubeGlobalConfigValues.DebugString())
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.Name, err)
			}

			h.moduleManager.UpdateGlobalConfigValues(configValuesPatchResult.Values)

			logEntry.Debugf("Global hook '%s': kube config global values updated", h.Name)
			logEntry.Debugf("New kube config global values:\n%s\n", h.moduleManager.kubeGlobalConfigValues.DebugString())
		}
		// Apply patches for *Enabled keys.
		err = h.applyEnabledPatches(*configValuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global config patch: %v", err)
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		globalValues, err := h.moduleManager.GlobalValues()
		if err != nil {
			return fmt.Errorf("global hook '%s': global values before patch apply: %s", h.Name, err)
		}

		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := h.handleGlobalValuesPatch(globalValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.Name, err)
		}

		// MemoryValuesPatch from global hook can contains patches for *Enabled keys
		// and no patches for 'global' section — valuesPatchResult will be nil in this case.
		if valuesPatchResult != nil && valuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global values before update", h.Name)
			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalValues(valuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply values patch for global values"),
					validationErr,
				)
			}

			h.moduleManager.UpdateGlobalDynamicValuesPatches(valuesPatchResult.ValuesPatch)
			newGlobalValues, err := h.moduleManager.GlobalValues()
			if err != nil {
				return fmt.Errorf("global hook '%s': global values after patch apply: %s", h.Name, err)
			}
			logEntry.Debugf("Global hook '%s': kube global values updated", h.Name)
			logEntry.Debugf("New global values:\n%s", newGlobalValues.DebugString())
		}
		// Apply patches for *Enabled keys.
		err = h.applyEnabledPatches(*valuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global values patch: %v", err)
		}
	}

	return nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *GlobalHook) PrepareTmpFilesForHookRun(bindingContext []byte) (tmpFiles map[string]string, err error) {
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

func (h *GlobalHook) GetConfigValues() utils.Values {
	return h.moduleManager.GlobalConfigValues()
}

// CONFIG_VALUES_PATH
func (h *GlobalHook) prepareConfigValuesJsonFile() (string, error) {
	configValues := h.GetConfigValues()
	data, err := configValues.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-config-values-%s.json", h.SafeName(), uuid.NewV4().String()))
	err = dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s config values:\n%s", h.Name, configValues.DebugString())

	return path, nil
}

func (h *GlobalHook) GetValues() (utils.Values, error) {
	return h.moduleManager.GlobalValues()
}

// VALUES_PATH
func (h *GlobalHook) prepareValuesJsonFile() (filePath string, err error) {
	values, err := h.GetValues()
	if err != nil {
		return "", nil
	}
	data, err := values.JsonBytes()
	if err != nil {
		return "", err
	}

	filePath = filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-values-%s.json", h.SafeName(), uuid.NewV4().String()))
	err = dumpData(filePath, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s values:\n%s", h.Name, values.DebugString())

	return filePath, nil
}

// BINDING_CONTEXT_PATH
func (h *GlobalHook) prepareBindingContextJsonFile(bindingContext []byte) (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-binding-context-%s.json", h.SafeName(), uuid.NewV4().String()))
	err := dumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	// TODO too much information in binding context with snapshots!
	// log.Debugf("Prepared global hook %s binding context:\n%s", h.Name, string(bindingContext))

	return path, nil
}

// CONFIG_VALUES_JSON_PATCH_PATH
func (h *GlobalHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-config-values-%s.json-patch", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// VALUES_JSON_PATCH_PATH
func (h *GlobalHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-values-%s.json-patch", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// METRICS_PATH
func (h *GlobalHook) prepareMetricsFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-metrics-%s.json", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// KUBERNETES PATCH PATH
func (h *GlobalHook) prepareKubernetesPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s-object-patch-%s", h.SafeName(), uuid.NewV4().String()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}
