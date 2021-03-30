package module_manager

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	uuid "gopkg.in/satori/go.uuid.v1"

	"github.com/flant/shell-operator/pkg/hook"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/sdk"
)

type GlobalHook struct {
	*CommonHook
	Config *GlobalHookConfig
}

var _ Hook = &GlobalHook{}

func NewGlobalHook(name, path string) *GlobalHook {
	res := &GlobalHook{
		CommonHook: &CommonHook{
			KubernetesBindingSynchronizationState: make(map[string]*KubernetesBindingSynchronizationState),
		},
		Config: &GlobalHookConfig{},
	}
	res.Name = name
	res.Path = path
	return res
}

func (g *GlobalHook) WithConfig(configOutput []byte) (err error) {
	err = g.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load global hook '%s' config: %s\nhook --config output: %s", g.Name, err.Error(), configOutput)
	}
	// Make HookController and GetConfigDescription work.
	g.Hook.Config = &g.Config.HookConfig
	g.Hook.RateLimiter = hook.CreateRateLimiter(g.Hook.Config)

	return nil
}

func (g *GlobalHook) WithGoConfig(config *sdk.HookConfig) (err error) {
	g.Config = NewGlobalHookConfigFromGoConfig(config)
	// Make HookController and GetConfigDescription work.
	g.Hook.Config = &g.Config.HookConfig
	return nil
}

func (gh *GlobalHook) GetConfigDescription() string {
	msgs := []string{}
	if gh.Config.BeforeAll != nil {
		msgs = append(msgs, fmt.Sprintf("beforeAll:%d", int64(gh.Config.BeforeAll.Order)))
	}
	if gh.Config.AfterAll != nil {
		msgs = append(msgs, fmt.Sprintf("afterAll:%d", int64(gh.Config.AfterAll.Order)))
	}
	msgs = append(msgs, gh.Hook.GetConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (g *GlobalHook) Order(binding BindingType) float64 {
	if g.Config.HasBinding(binding) {
		switch binding {
		case BeforeAll:
			return g.Config.BeforeAll.Order
		case AfterAll:
			return g.Config.AfterAll.Order
		case OnStartup:
			return g.Config.OnStartup.Order
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
	//versionedContextList := ConvertBindingContextList(h.Config.Version, bindingContext)

	globalHookExecutor := NewHookExecutor(h, bindingContext, h.Config.Version)
	globalHookExecutor.WithLogLabels(logLabels)
	hookResult, err := globalHookExecutor.Run()
	if hookResult != nil && hookResult.Usage != nil {
		metricLabels := map[string]string{
			"hook":       h.Name,
			"binding":    string(bindingType),
			"queue":      logLabels["queue"],
			"activation": logLabels["event.type"],
		}
		// usage metrics
		h.moduleManager.metricStorage.HistogramObserve("{PREFIX}global_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels)
		h.moduleManager.metricStorage.HistogramObserve("{PREFIX}global_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels)
		h.moduleManager.metricStorage.GaugeSet("{PREFIX}global_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		return fmt.Errorf("global hook '%s' failed: %s", h.Name, err)
	}

	// Apply metric operations
	err = h.moduleManager.hookMetricStorage.SendBatch(hookResult.Metrics, map[string]string{
		"hook": h.Name,
	})
	if err != nil {
		return err
	}

	if len(hookResult.KubernetesPatchBytes) > 0 {
		err = h.moduleManager.KubeObjectPatcher.GenerateFromJSONAndExecuteOperations(hookResult.KubernetesPatchBytes)
		if err != nil {
			return err
		}
	}

	//h.moduleManager.ValuesLock.Lock()
	//defer h.moduleManager.ValuesLock.Unlock()

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
			log.Debugf("Global hook '%s': validate global config values before update", h.Name)
			// Validate merged static and new values.
			mergedValues := h.moduleManager.GlobalStaticAndNewValues(configValuesPatchResult.Values)
			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalConfigValues(mergedValues)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply config values patch for global values"),
					validationErr,
				)
			}

			err := h.moduleManager.kubeConfigManager.SetKubeGlobalValues(configValuesPatchResult.Values)
			if err != nil {
				log.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.Name, h.moduleManager.kubeGlobalConfigValues.DebugString())
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.Name, err)
			}

			h.moduleManager.kubeGlobalConfigValues = configValuesPatchResult.Values
			log.Debugf("Global hook '%s': kube config global values updated:\n%s", h.Name, h.moduleManager.kubeGlobalConfigValues.DebugString())
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
			log.Debugf("Global hook '%s': validate global values before update", h.Name)
			validationErr := h.moduleManager.ValuesValidator.ValidateGlobalValues(valuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply values patch for global values"),
					validationErr,
				)
			}

			h.moduleManager.globalDynamicValuesPatches = utils.AppendValuesPatch(
				h.moduleManager.globalDynamicValuesPatches,
				valuesPatchResult.ValuesPatch)
			newGlobalValues, err := h.moduleManager.GlobalValues()
			if err != nil {
				return fmt.Errorf("global hook '%s': global values after patch apply: %s", h.Name, err)
			}
			log.Debugf("Global hook '%s': global values updated:\n%s", h.Name, newGlobalValues.DebugString())
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
	var configValues = h.GetConfigValues()
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
	//log.Debugf("Prepared global hook %s binding context:\n%s", h.Name, string(bindingContext))

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
