package module_manager

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/romana/rlog"

	hook2 "github.com/flant/shell-operator/pkg/hook"
	utils_data "github.com/flant/shell-operator/pkg/utils/data"

	"github.com/flant/addon-operator/pkg/utils"
)

type ModuleHook struct {
	*CommonHook
	Module *Module
	Config *ModuleHookConfig
}
var _ Hook = &ModuleHook{}

func NewModuleHook(name, path string) *ModuleHook {
	return &ModuleHook{
		CommonHook: &CommonHook{
			Name: name,
			Path: path,
		},
		Config: &ModuleHookConfig{},
	}
}

func (m *ModuleHook) WithModule(module *Module) {
	m.Module = module
}

func (m *ModuleHook) WithConfig(configOutput []byte) (err error) {
	err = m.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load module hook '%s' config: %s\nhook --config output: %s", m.Name, err.Error(), configOutput)
	}
	return nil
}

func (m *ModuleHook) Order(binding BindingType) float64 {
	if m.Config.HasBinding(binding) {
		switch binding {
		case BeforeHelm:
			return m.Config.BeforeHelm.Order
		case AfterHelm:
			return m.Config.AfterHelm.Order
		case AfterDeleteHelm:
			return m.Config.AfterDeleteHelm.Order
		}
	}
	return 0.0
}


type moduleValuesMergeResult struct {
	// global values with root ModuleValuesKey key
	Values utils.Values
	// global values under root ModuleValuesKey key
	ModuleValues    map[string]interface{}
	ModuleValuesKey string
	ValuesPatch     utils.ValuesPatch
	ValuesChanged   bool
}


func (h *ModuleHook) handleModuleValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*moduleValuesMergeResult, error) {
	moduleValuesKey := utils.ModuleNameToValuesKey(h.Module.Name)

	if err := utils.ValidateHookValuesPatch(valuesPatch, moduleValuesKey); err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", h.Module.Name, err)
	}

	newValuesRaw, valuesChanged, err := utils.ApplyValuesPatch(currentValues, valuesPatch)
	if err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", h.Module.Name, err)
	}

	result := &moduleValuesMergeResult{
		ModuleValuesKey: moduleValuesKey,
		Values:          utils.Values{moduleValuesKey: make(map[string]interface{})},
		ValuesChanged:   valuesChanged,
		ValuesPatch:     valuesPatch,
	}

	if moduleValuesRaw, hasKey := newValuesRaw[result.ModuleValuesKey]; hasKey {
		moduleValues, ok := moduleValuesRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map at key '%s', got:\n%s", result.ModuleValuesKey, utils_data.YamlToString(moduleValuesRaw))
		}
		result.Values[result.ModuleValuesKey] = moduleValues
		result.ModuleValues = moduleValues
	}

	return result, nil
}

func (h *ModuleHook) run(bindingType BindingType, context []BindingContext) error {
	moduleName := h.Module.Name
	rlog.Infof("Running module hook '%s' binding '%s' ...", h.Name, bindingType)

	// Convert context for version
	versionedContext := make([]interface{}, 0, len(context))
	for _, c := range context {
		versionedContext = append(versionedContext, hook2.ConvertBindingContext(h.Config.Version, c.BindingContext))
	}

	moduleHookExecutor := NewHookExecutor(h, versionedContext)
	patches, err := moduleHookExecutor.Run()
	if err != nil {
		return fmt.Errorf("module hook '%s' failed: %s", h.Name, err)
	}

	configValuesPatch, has := patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil{
		preparedConfigValues := utils.MergeValues(
			utils.Values{utils.ModuleNameToValuesKey(moduleName): map[string]interface{}{}},
			h.moduleManager.kubeModulesConfigValues[moduleName],
		)

		configValuesPatchResult, err := h.handleModuleValuesPatch(preparedConfigValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': kube module config values update error: %s", h.Name, err)
		}
		if configValuesPatchResult.ValuesChanged {
			err := h.moduleManager.kubeConfigManager.SetKubeModuleValues(moduleName, configValuesPatchResult.Values)
			if err != nil {
				rlog.Debugf("Module hook '%s' kube module config values stay unchanged:\n%s", utils.ValuesToString(h.moduleManager.kubeModulesConfigValues[moduleName]))
				return fmt.Errorf("module hook '%s': set kube module config failed: %s", h.Name, err)
			}

			h.moduleManager.kubeModulesConfigValues[moduleName] = configValuesPatchResult.Values
			rlog.Debugf("Module hook '%s': kube module '%s' config values updated:\n%s", h.Name, moduleName, utils.ValuesToString(h.moduleManager.kubeModulesConfigValues[moduleName]))
		}
	}

	valuesPatch, has := patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		valuesPatchResult, err := h.handleModuleValuesPatch(h.values(), *valuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': dynamic module values update error: %s", h.Name, err)
		}
		if valuesPatchResult.ValuesChanged {
			h.moduleManager.modulesDynamicValuesPatches[moduleName] = utils.AppendValuesPatch(h.moduleManager.modulesDynamicValuesPatches[moduleName], valuesPatchResult.ValuesPatch)
			rlog.Debugf("Module hook '%s': dynamic module '%s' values updated:\n%s", h.Name, moduleName, utils.ValuesToString(h.values()))
		}
	}

	return nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *ModuleHook) PrepareTmpFilesForHookRun(context interface{}) (tmpFiles map[string]string, err error) {
	tmpFiles = make(map[string]string, 0)

	tmpFiles["CONFIG_VALUES_PATH"], err = h.prepareConfigValuesJsonFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_PATH"], err = h.prepareValuesJsonFile()
	if err != nil {
		return
	}

	tmpFiles["BINDING_CONTEXT_PATH"], err = h.prepareBindingContextJsonFile(context)
	if err != nil {
		return
	}

	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err= h.prepareConfigValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_JSON_PATCH_PATH"], err = h.prepareValuesJsonPatchFile()
	if err != nil {
		return
	}

	return
}


func (h *ModuleHook) configValues() utils.Values {
	return h.Module.configValues()
}

func (h *ModuleHook) values() utils.Values {
	return h.Module.values()
}

func (h *ModuleHook) prepareValuesJsonFile() (string, error) {
	return h.Module.prepareValuesJsonFile()
}

func (h *ModuleHook) prepareValuesYamlFile() (string, error) {
	return h.Module.prepareValuesYamlFile()
}

func (h *ModuleHook) prepareConfigValuesJsonFile() (string, error) {
	return h.Module.prepareConfigValuesJsonFile()
}

func (h *ModuleHook) prepareConfigValuesYamlFile() (string, error) {
	return h.Module.prepareConfigValuesYamlFile()
}

func (h *ModuleHook) prepareBindingContextJsonFile(context interface{}) (string, error) {
	data, _ := json.Marshal(context)
	//data := utils.MustDump(utils.DumpValuesJson(context))
	path := filepath.Join(h.moduleManager.TempDir, fmt.Sprintf("%s.module-hook-%s-binding-context.json", h.Module.SafeName(), h.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	rlog.Debugf("Prepared module %s hook %s binding context:\n%s", h.Module.SafeName(), h.Name, utils_data.YamlToString(context))

	return path, nil
}


func (h *ModuleHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.moduleManager.TempDir, fmt.Sprintf("%s.global-hook-config-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (h *ModuleHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.moduleManager.TempDir, fmt.Sprintf("%s.global-hook-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}
