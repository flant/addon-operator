package module_manager

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	log "github.com/sirupsen/logrus"
)

func (mm *ModuleManager) loadGlobalValues() ( /* global values */ utils.Values /* enabled modules */, map[string]struct{}, error) {
	resultGlobalValues := utils.Values{}
	enabledModules := make(map[string]struct{})

	dirs := utils.SplitToPaths(mm.ModulesDir)
	for _, dir := range dirs {
		commonStaticValues, err := utils.LoadValuesFileFromDir(dir)
		if err != nil {
			return nil, nil, err
		}

		for key, value := range commonStaticValues {
			if key == utils.GlobalValuesKey {
				section := commonStaticValues.GetKeySection(utils.GlobalValuesKey)
				resultGlobalValues = utils.MergeValues(resultGlobalValues, section)
				continue
			}

			if strings.HasSuffix(key, "Enabled") {
				enabled := false

				switch v := value.(type) {
				case bool:
					enabled = v
				case string:
					enabled, _ = strconv.ParseBool(v)
				default:
					return nil, nil, fmt.Errorf("unknown type for Enabled flag: %s: %T", key, value)
				}

				if enabled {
					moduleName := utils.ModuleNameFromValuesKey(strings.TrimSuffix(key, "Enabled"))
					enabledModules[moduleName] = struct{}{}
				}
			}
		}
	}

	cb, vb, err := utils.ReadOpenAPIFiles(filepath.Join(mm.GlobalHooksDir, "openapi"))
	if err != nil {
		return nil, nil, err
	}

	if cb != nil && vb != nil {
		// set openapi global spec to storage
		err = mm.ValuesValidator.SchemaStorage.AddGlobalValuesSchemas(cb, vb)
		if err != nil {
			return nil, nil, err
		}
	}

	return resultGlobalValues, enabledModules, nil
}

func (mm *ModuleManager) registerGlobalModule(globalValues utils.Values) error {
	log.Infof(mm.ValuesValidator.SchemaStorage.GlobalSchemasDescription())

	// load and registry global hooks
	dep := hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
	}

	gm := modules.NewGlobalModule(mm.GlobalHooksDir, globalValues, mm.ValuesValidator, &dep)
	mm.global = gm

	// catch dynamin Enabled patches from global hooks
	go mm.runDynamicEnabledLoop()

	return mm.registerGlobalHooks(gm)
}

func (mm *ModuleManager) registerGlobalHooks(gm *modules.GlobalModule) error {
	log.Debug("Search and register global hooks")

	hks, err := gm.RegisterHooks()
	if err != nil {
		return err
	}

	for _, hk := range hks {
		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(hk.GetHookConfig().OnKubernetesEvents, mm.dependencies.KubeEventsManager)
		hookCtrl.InitScheduleBindings(hk.GetHookConfig().Schedules, mm.dependencies.ScheduleManager)

		hk.WithHookController(hookCtrl)
		hk.WithTmpDir(mm.TempDir)

		mm.dependencies.MetricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(hk.GetHookConfig().BindingsCount()),
			map[string]string{
				"hook":   hk.GetName(),
				"module": "", // empty "module" label for label set consistency with module hooks
			})
	}

	return nil
}

func (mm *ModuleManager) RegisterModuleHooks(ml *modules.BasicModule, logLabels map[string]string) error {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels)).WithField("module", ml.Name)

	hks, err := ml.RegisterHooks(logEntry)
	if err != nil {
		return err
	}

	for _, hk := range hks {
		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(hk.GetHookConfig().OnKubernetesEvents, mm.dependencies.KubeEventsManager)
		hookCtrl.InitScheduleBindings(hk.GetHookConfig().Schedules, mm.dependencies.ScheduleManager)

		hk.WithHookController(hookCtrl)
		hk.WithTmpDir(mm.TempDir)

		mm.dependencies.MetricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(hk.GetHookConfig().BindingsCount()),
			map[string]string{
				"module": ml.Name,
				"hook":   hk.GetName(),
			})
	}

	return nil
}

// RunOnStartup is a phase of module lifecycle that runs onStartup hooks.
// It is a handler of task MODULE_RUN
// Run is a phase of module lifecycle that runs onStartup and beforeHelm hooks, helm upgrade --install command and afterHelm hook.
// It is a handler of task MODULE_RUN
func (mm *ModuleManager) RunModuleHooks(m *modules.BasicModule, bt sh_op_types.BindingType, logLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": m.Name,
		"queue":  "main",
	})

	m.SetStateEnabled(true)

	// Hooks can delete release resources, so stop resources monitor before run hooks.
	// m.moduleManager.HelmResourcesManager.PauseMonitor(m.Name)

	return m.RunHooksByBinding(bt, logLabels)
}

//func (mm *ModuleManager) RegisterModuleHooks(module *Module, logLabels map[string]string) error {
//	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels)).WithField("module", module.Name)
//
//	if _, ok := mm.modulesHooksOrderByName[module.Name]; ok {
//		logEntry.Debugf("Module hooks already registered")
//		return nil
//	}
//	logEntry.Debugf("Search and register hooks")
//
//	registeredModuleHooks := make(map[sh_op_types.BindingType][]*ModuleHook)
//
//	hks, err := searchModuleHooks(module)
//	if err != nil {
//		logEntry.Errorf("Search module hooks: %s", err)
//		return err
//	}
//	logEntry.Debugf("Found %d hooks", len(hks))
//	for _, h := range hks {
//		logEntry.Debugf("  ModuleHook: Name=%s, Path=%s", h.Name, h.Path)
//	}
//
//	for _, moduleHook := range hks {
//		hookLogEntry := logEntry.WithField("hook", moduleHook.Name).
//			WithField("hook.type", "module")
//
//		var yamlConfigBytes []byte
//		var goConfig *go_hook.HookConfig
//
//		if moduleHook.GoHook != nil {
//			goConfig = moduleHook.GoHook.Config()
//		} else {
//			hookExecutor := NewHookExecutor(moduleHook, nil, "", nil)
//			hookExecutor.WithHelm(mm.dependencies.Helm)
//			yamlConfigBytes, err = hookExecutor.Config()
//			if err != nil {
//				hookLogEntry.Errorf("Run --config: %s", err)
//				return fmt.Errorf("module hook --config run problem")
//			}
//		}
//
//		if len(yamlConfigBytes) > 0 {
//			err = moduleHook.WithConfig(yamlConfigBytes)
//			if err != nil {
//				hookLogEntry.Errorf("Hook return bad config: %s", err)
//				return fmt.Errorf("module hook return bad config")
//			}
//		} else if moduleHook.GoHook != nil {
//			err := moduleHook.WithGoConfig(goConfig)
//			if err != nil {
//				logEntry.Errorf("Hook return bad config: %s", err)
//				return fmt.Errorf("module hook return bad config")
//			}
//		}
//
//		moduleHook.WithModuleManager(mm)
//
//		// Add hook info as log labels
//		for _, kubeCfg := range moduleHook.Config.OnKubernetesEvents {
//			kubeCfg.Monitor.Metadata.LogLabels["module"] = module.Name
//			kubeCfg.Monitor.Metadata.LogLabels["hook"] = moduleHook.Name
//			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "module"
//			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
//				"hook":    moduleHook.Name,
//				"binding": kubeCfg.BindingName,
//				"module":  module.Name,
//				"queue":   kubeCfg.Queue,
//				"kind":    kubeCfg.Monitor.Kind,
//			}
//		}
//
//		hookCtrl := controller.NewHookController()
//		hookCtrl.InitKubernetesBindings(moduleHook.Config.OnKubernetesEvents, mm.dependencies.KubeEventsManager)
//		hookCtrl.InitScheduleBindings(moduleHook.Config.Schedules, mm.dependencies.ScheduleManager)
//
//		moduleHook.WithHookController(hookCtrl)
//		moduleHook.WithTmpDir(mm.TempDir)
//
//		// register module hook in indexes
//		for _, binding := range moduleHook.Config.Bindings() {
//			registeredModuleHooks[binding] = append(registeredModuleHooks[binding], moduleHook)
//		}
//
//		hookLogEntry.Infof("Module hook from '%s'. Bindings: %s", moduleHook.Path, moduleHook.GetConfigDescription())
//
//		mm.dependencies.MetricStorage.GaugeSet(
//			"{PREFIX}binding_count",
//			float64(moduleHook.Config.BindingsCount()),
//			map[string]string{
//				"module": module.Name,
//				"hook":   moduleHook.Name,
//			})
//	}
//
//	// Save registered hooks in mm.modulesHooksOrderByName
//	if mm.modulesHooksOrderByName[module.Name] == nil {
//		mm.modulesHooksOrderByName[module.Name] = make(map[sh_op_types.BindingType][]*ModuleHook)
//	}
//	mm.modulesHooksOrderByName[module.Name] = registeredModuleHooks
//
//	return nil
//}

//func searchModuleHooks(module *Module) (hks []*ModuleHook, err error) {
//	hks = make([]*ModuleHook, 0)
//
//	shellHooks, err := searchModuleShellHooks(module)
//	if err != nil {
//		return nil, err
//	}
//	hks = append(hks, shellHooks...)
//
//	goHooks, err := searchModuleGoHooks(module)
//	if err != nil {
//		return nil, err
//	}
//	hks = append(hks, goHooks...)
//
//	sort.SliceStable(hks, func(i, j int) bool {
//		return hks[i].Path < hks[j].Path
//	})
//
//	return hks, nil
//}

//func searchModuleShellHooks(module *Module) (hks []*ModuleHook, err error) {
//	hooksDir := filepath.Join(module.Path, "hooks")
//	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
//		return nil, nil
//	}
//
//	hooksRelativePaths, err := utils_file.RecursiveGetExecutablePaths(hooksDir)
//	if err != nil {
//		return nil, err
//	}
//
//	hks = make([]*ModuleHook, 0)
//
//	// sort hooks by path
//	sort.Strings(hooksRelativePaths)
//	log.Debugf("  Hook paths: %+v", hooksRelativePaths)
//
//	for _, hookPath := range hooksRelativePaths {
//		hookName, err := filepath.Rel(filepath.Dir(module.Path), hookPath)
//		if err != nil {
//			return nil, err
//		}
//
//		moduleHook := NewModuleHook(hookName, hookPath)
//		moduleHook.WithModule(module)
//
//		hks = append(hks, moduleHook)
//	}
//
//	return
//}
//
//func searchModuleGoHooks(module *Module) (hks []*ModuleHook, err error) {
//	// find module hooks in go hooks registry
//	hks = make([]*ModuleHook, 0)
//	goHooks := sdk.Registry().Hooks()
//	for _, h := range goHooks {
//		m := h.Metadata
//		if !m.Module {
//			continue
//		}
//		if m.ModuleName != module.Name {
//			continue
//		}
//
//		moduleHook := NewModuleHook(m.Name, m.Path)
//		moduleHook.WithModule(module)
//		moduleHook.WithGoHook(h.Hook)
//
//		hks = append(hks, moduleHook)
//	}
//
//	return hks, nil
//}
