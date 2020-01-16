package module_manager

import (
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	"github.com/flant/addon-operator/pkg/utils"
	utils_data "github.com/flant/addon-operator/pkg/utils/data"
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
		Config: &ModuleHookConfig{},
	}
	res.Name = name
	res.Path = path
	return res
}

func (m *ModuleHook) WithModule(module *Module) {
	m.Module = module
}

func (m *ModuleHook) WithConfig(configOutput []byte) (err error) {
	err = m.Config.LoadAndValidate(configOutput)
	if err != nil {
		return fmt.Errorf("load module hook '%s' config: %s\nhook --config output: %s", m.Name, err.Error(), configOutput)
	}
	// Make HookController and GetConfigDescription work.
	m.Hook.Config = &m.Config.HookConfig
	return nil
}

func (m *ModuleHook) GetConfigDescription() string {
	msgs := []string{}
	if m.Config.BeforeHelm != nil {
		msgs = append(msgs, fmt.Sprintf("beforeHelm:%d", int64(m.Config.BeforeHelm.Order)))
	}
	if m.Config.AfterHelm != nil {
		msgs = append(msgs, fmt.Sprintf("afterHelm:%d", int64(m.Config.AfterHelm.Order)))
	}
	if m.Config.AfterDeleteHelm != nil {
		msgs = append(msgs, fmt.Sprintf("afterDeleteHelm:%d", int64(m.Config.AfterDeleteHelm.Order)))
	}
	msgs = append(msgs, m.Hook.GetConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (m *ModuleHook) Order(binding BindingType) float64 {
	if m.Config.HasBinding(binding) {
		switch binding {
		case OnStartup:
			return m.Config.OnStartup.Order
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

func (h *ModuleHook) Run(bindingType BindingType, context []BindingContext, logLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"hook": h.Name,
		"hook.type": "module",
		"binding": string(bindingType),
	})

	// Convert context for version
	versionedContextList := ConvertBindingContextList(h.Config.Version, context)

	moduleHookExecutor := NewHookExecutor(h, versionedContextList)
	moduleHookExecutor.WithLogLabels(logLabels)
	patches, err := moduleHookExecutor.Run()
	if err != nil {
		return fmt.Errorf("module hook '%s' failed: %s", h.Name, err)
	}

	moduleName := h.Module.Name

	// ValuesLock.Lock()
	// defer ValuesLock.UnLock()
	//h.moduleManager.ValuesLock.Lock()

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
				log.Debugf("Module hook '%s' kube module config values stay unchanged:\n%s", h.Name, utils.ValuesToString(h.moduleManager.kubeModulesConfigValues[moduleName]))
				return fmt.Errorf("module hook '%s': set kube module config failed: %s", h.Name, err)
			}

			h.moduleManager.kubeModulesConfigValues[moduleName] = configValuesPatchResult.Values
			log.Debugf("Module hook '%s': kube module '%s' config values updated:\n%s", h.Name, moduleName, utils.ValuesToString(h.moduleManager.kubeModulesConfigValues[moduleName]))
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
			log.Debugf("Module hook '%s': dynamic module '%s' values updated:\n%s", h.Name, moduleName, utils.ValuesToString(h.values()))
		}
	}

	return nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *ModuleHook) PrepareTmpFilesForHookRun(bindingContext []byte) (tmpFiles map[string]string, err error) {
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

func (h *ModuleHook) prepareBindingContextJsonFile(bindingContext []byte) (string, error) {
	//data := utils.MustDump(utils.DumpValuesJson(context))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-%s-binding-context.json", h.Module.SafeName(), h.SafeName()))
	err := dumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	// FIXME too much information because of snapshots
	//log.Debugf("Prepared module %s hook %s binding context:\n%s", h.Module.SafeName(), h.Name, string(bindingContext))

	return path, nil
}


func (h *ModuleHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-config-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (h *ModuleHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}
