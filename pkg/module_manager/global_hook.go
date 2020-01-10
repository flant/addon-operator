package module_manager

import (
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	. "github.com/flant/addon-operator/pkg/hook/types"

	"github.com/flant/addon-operator/pkg/utils"
	utils_data "github.com/flant/addon-operator/pkg/utils/data"
)

type GlobalHook struct {
	*CommonHook
	Config *GlobalHookConfig
}
var _ Hook = &GlobalHook{}

func NewGlobalHook(name, path string) *GlobalHook {
	res := &GlobalHook{
		CommonHook: &CommonHook{},
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

func (g *GlobalHook) Order(binding BindingType) float64 {
	if g.Config.HasBinding(binding) {
		switch binding {
		case BeforeAll:
			return g.Config.BeforeAll.Order
		case AfterAll:
			return g.Config.AfterAll.Order
		}
	}
	return 0.0
}

type globalValuesMergeResult struct {
	// Global values with the root "global" key.
	Values utils.Values
	// Global values under the root "global" key.
	GlobalValues map[string]interface{}
	// Original values patch argument.
	ValuesPatch utils.ValuesPatch
	// Whether values changed after applying patch.
	ValuesChanged bool
}

func (h *GlobalHook) handleGlobalValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*globalValuesMergeResult, error) {
	acceptableKey := "global"

	if err := utils.ValidateHookValuesPatch(valuesPatch, acceptableKey); err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	newValuesRaw, valuesChanged, err := utils.ApplyValuesPatch(currentValues, valuesPatch)
	if err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	result := &globalValuesMergeResult{
		Values:        utils.Values{acceptableKey: make(map[string]interface{})},
		ValuesChanged: valuesChanged,
		ValuesPatch:   valuesPatch,
	}

	if globalValuesRaw, hasKey := newValuesRaw[acceptableKey]; hasKey {
		globalValues, ok := globalValuesRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map at key '%s', got:\n%s", acceptableKey, utils_data.YamlToString(globalValuesRaw))
		}

		result.Values[acceptableKey] = globalValues
		result.GlobalValues = globalValues
	}

	return result, nil
}

func (h *GlobalHook) Run(bindingType BindingType, context []BindingContext, logLabels map[string]string) error {
	// Convert context for version
	versionedContextList := ConvertBindingContextList(h.Config.Version, context)

	globalHookExecutor := NewHookExecutor(h, versionedContextList)
	globalHookExecutor.WithLogLabels(logLabels)
	patches, err := globalHookExecutor.Run()
	if err != nil {
		return fmt.Errorf("global hook '%s' failed: %s", h.Name, err)
	}

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
				log.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.Name, utils.ValuesToString(h.moduleManager.kubeGlobalConfigValues))
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.Name, err)
			}

			h.moduleManager.kubeGlobalConfigValues = configValuesPatchResult.Values
			log.Debugf("Global hook '%s': kube config global values updated:\n%s", h.Name, utils.ValuesToString(h.moduleManager.kubeGlobalConfigValues))
		}
	}

	valuesPatch, has := patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		valuesPatchResult, err := h.handleGlobalValuesPatch(h.values(), *valuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.Name, err)
		}
		if valuesPatchResult.ValuesChanged {
			h.moduleManager.globalDynamicValuesPatches = utils.AppendValuesPatch(h.moduleManager.globalDynamicValuesPatches, valuesPatchResult.ValuesPatch)
			log.Debugf("Global hook '%s': global values updated:\n%s", h.Name, utils.ValuesToString(h.values()))
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

	return
}


func (h *GlobalHook) configValues() utils.Values {
	return utils.MergeValues(
		utils.Values{"global": map[string]interface{}{}},
		h.moduleManager.kubeGlobalConfigValues,
	)
}

func (h *GlobalHook) values() utils.Values {
	var err error

	res := utils.MergeValues(
		utils.Values{"global": map[string]interface{}{}},
		h.moduleManager.globalCommonStaticValues,
		h.moduleManager.kubeGlobalConfigValues,
	)

	// Invariant: do not store patches that does not apply
	// Give user error for patches early, after patch receive
	for _, patch := range h.moduleManager.globalDynamicValuesPatches {
		res, _, err = utils.ApplyValuesPatch(res, patch)
		if err != nil {
			panic(err)
		}
	}

	return res
}

func (h *GlobalHook) prepareConfigValuesYamlFile() (string, error) {
	values := h.configValues()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-config-values.yaml", h.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s config values:\n%s", h.Name, utils.ValuesToString(values))

	return path, nil
}

func (h *GlobalHook) prepareConfigValuesJsonFile() (string, error) {
	values := h.configValues()

	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-config-values.json", h.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s config values:\n%s", h.Name, utils.ValuesToString(values))

	return path, nil
}

func (h *GlobalHook) prepareValuesYamlFile() (string, error) {
	values := h.values()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-values.yaml", h.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s values:\n%s", h.Name, utils.ValuesToString(values))

	return path, nil
}

func (h *GlobalHook) prepareValuesJsonFile() (string, error) {
	values := h.values()

	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-values.json", h.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s values:\n%s", h.Name, utils.ValuesToString(values))

	return path, nil
}

func (h *GlobalHook) prepareBindingContextJsonFile(bindingContext []byte) (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("global-hook-%s-binding-context.json", h.SafeName()))
	err := dumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared global hook %s binding context:\n%s", h.Name, string(bindingContext))

	return path, nil
}


func (h *GlobalHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-config-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (h *GlobalHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.global-hook-values.json-patch", h.SafeName()))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}
