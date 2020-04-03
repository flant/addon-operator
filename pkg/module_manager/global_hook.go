package module_manager

import (
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/satori/go.uuid.v1"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	. "github.com/flant/addon-operator/pkg/hook/types"

	"github.com/flant/addon-operator/pkg/utils"
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

func (g *GlobalHook) WithConfig(configOutput []byte) (err error) {
	err = g.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load global hook '%s' config: %s\nhook --config output: %s", g.Name, err.Error(), configOutput)
	}
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

type globalValuesMergeResult struct {
	// Global values with the root "global" key.
	Values utils.Values
	// Original values patch argument.
	ValuesPatch utils.ValuesPatch
	// Whether values changed after applying patch.
	ValuesChanged bool
}

func (h *GlobalHook) handleGlobalValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*globalValuesMergeResult, error) {

	if err := utils.ValidateHookValuesPatch(valuesPatch, utils.GlobalValuesKey); err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	newValues, valuesChanged, err := utils.ApplyValuesPatch(currentValues, valuesPatch)
	if err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	result := &globalValuesMergeResult{
		Values:        utils.Values{utils.GlobalValuesKey: make(map[string]interface{})},
		ValuesChanged: valuesChanged,
		ValuesPatch:   valuesPatch,
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

func (h *GlobalHook) Run(bindingType BindingType, context []BindingContext, logLabels map[string]string) error {
	// Convert context for version
	versionedContextList := ConvertBindingContextList(h.Config.Version, context)

	globalHookExecutor := NewHookExecutor(h, versionedContextList)
	globalHookExecutor.WithLogLabels(logLabels)
	patches, metrics, err := globalHookExecutor.Run()
	if err != nil {
		return fmt.Errorf("global hook '%s' failed: %s", h.Name, err)
	}

	// Apply metric operations
	err = h.moduleManager.metricStorage.SendBatch(metrics, map[string]string{
		"hook": h.Name,
	})
	if err != nil {
		return err
	}

	//h.moduleManager.ValuesLock.Lock()
	//defer h.moduleManager.ValuesLock.Unlock()

	configValuesPatch, has := patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil {
		preparedConfigValues := utils.MergeValues(
			utils.Values{"global": map[string]interface{}{}},
			h.moduleManager.kubeGlobalConfigValues,
		)

		configValuesPatchResult, err := h.handleGlobalValuesPatch(preparedConfigValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': kube config global values update error: %s", h.Name, err)
		}

		if configValuesPatchResult.ValuesChanged {
			err := h.moduleManager.kubeConfigManager.SetKubeGlobalValues(configValuesPatchResult.Values)
			if err != nil {
				log.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.Name, h.moduleManager.kubeGlobalConfigValues.DebugString())
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.Name, err)
			}

			h.moduleManager.kubeGlobalConfigValues = configValuesPatchResult.Values
			log.Debugf("Global hook '%s': kube config global values updated:\n%s", h.Name, h.moduleManager.kubeGlobalConfigValues.DebugString())
		}
	}

	valuesPatch, has := patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		globalValues, err := h.moduleManager.GlobalValues()
		if err != nil {
			return fmt.Errorf("global hook '%s': global values before patch apply: %s", h.Name, err)
		}
		valuesPatchResult, err := h.handleGlobalValuesPatch(globalValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.Name, err)
		}
		if valuesPatchResult.ValuesChanged {
			h.moduleManager.globalDynamicValuesPatches = utils.AppendValuesPatch(h.moduleManager.globalDynamicValuesPatches, valuesPatchResult.ValuesPatch)
			newGlobalValues, err := h.moduleManager.GlobalValues()
			if err != nil {
				return fmt.Errorf("global hook '%s': global values after patch apply: %s", h.Name, err)
			}
			log.Debugf("Global hook '%s': global values updated:\n%s", h.Name, newGlobalValues.DebugString())
		}
	}

	return nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *GlobalHook) PrepareTmpFilesForHookRun(bindingContext []byte) (tmpFiles map[string]string, err error) {
	tmpFiles = make(map[string]string, 0)

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

	return
}

// CONFIG_VALUES_PATH
func (h *GlobalHook) prepareConfigValuesJsonFile() (string, error) {
	var configValues = h.moduleManager.GlobalConfigValues()
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

// VALUES_PATH
func (h *GlobalHook) prepareValuesJsonFile() (filePath string, err error) {
	values, err := h.moduleManager.GlobalValues()
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
