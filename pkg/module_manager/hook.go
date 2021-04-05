package module_manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/flant/addon-operator/sdk"
	log "github.com/sirupsen/logrus"

	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"

	"github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/hook/controller"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
)

type Hook interface {
	WithModuleManager(moduleManager *moduleManager)
	WithConfig(configOutput []byte) (err error)
	WithGoConfig(config *go_hook.HookConfig) (err error)
	WithHookController(hookController controller.HookController)
	GetName() string
	GetPath() string
	GetGoHook() go_hook.GoHook
	GetValues() (utils.Values, error)
	GetConfigValues() utils.Values
	PrepareTmpFilesForHookRun(bindingContext []byte) (map[string]string, error)
	Order(binding sh_op_types.BindingType) float64
}

type KubernetesBindingSynchronizationState struct {
	Queued bool
	Done   bool
}

func (k *KubernetesBindingSynchronizationState) String() string {
	return fmt.Sprintf("queue=%v done=%v", k.Queued, k.Done)
}

type CommonHook struct {
	hook.Hook

	moduleManager *moduleManager

	KubernetesBindingSynchronizationState map[string]*KubernetesBindingSynchronizationState

	GoHook go_hook.GoHook
}

func (c *CommonHook) WithModuleManager(moduleManager *moduleManager) {
	c.moduleManager = moduleManager
}

func (c *CommonHook) WithGoHook(h go_hook.GoHook) {
	c.GoHook = h
}

func (h *CommonHook) GetName() string {
	return h.Name
}

func (h *CommonHook) GetPath() string {
	return h.Path
}

func (h *CommonHook) GetGoHook() go_hook.GoHook {
	return h.GoHook
}

// SynchronizationNeeded is true if there is binding with executeHookOnSynchronization.
func (h *CommonHook) SynchronizationNeeded() bool {
	for _, kubeBinding := range h.Config.OnKubernetesEvents {
		if kubeBinding.ExecuteHookOnSynchronization {
			return true
		}
	}
	return false
}

// SynchronizationQueued is true if at least one KubernetesBindingSynchronizationState object has true for Queued.
func (h *CommonHook) SynchronizationQueued() bool {
	queued := false
	for _, state := range h.KubernetesBindingSynchronizationState {
		if state.Queued {
			queued = true
		}
	}
	return queued
}

// SynchronizationDone is true if all KubernetesBindingSynchronizationState objects has true for Done.
func (h *CommonHook) SynchronizationDone() bool {
	done := true
	for _, state := range h.KubernetesBindingSynchronizationState {
		if !state.Done {
			done = false
		}
	}
	return done
}

// SearchGlobalHooks recursively find all executables in hooksDir. Absent hooksDir is not an error.
func SearchGlobalHooks(hooksDir string) (hooks []*GlobalHook, err error) {
	hooks = make([]*GlobalHook, 0)
	shellHooks, err := SearchGlobalShellHooks(hooksDir)
	if err != nil {
		return nil, err
	}
	hooks = append(hooks, shellHooks...)

	goHooks, err := SearchGlobalGoHooks()
	if err != nil {
		return nil, err
	}
	hooks = append(hooks, goHooks...)

	sort.SliceStable(hooks, func(i, j int) bool {
		return hooks[i].Path < hooks[j].Path
	})

	log.Debugf("Search global hooks: %d shell, %d golang", len(shellHooks), len(goHooks))

	return hooks, nil
}

// SearchGlobalHooks recursively find all executables in hooksDir. Absent hooksDir is not an error.
func SearchGlobalShellHooks(hooksDir string) (hooks []*GlobalHook, err error) {
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
	}

	hooksSubDir := filepath.Join(hooksDir, "hooks")
	if _, err := os.Stat(hooksSubDir); !os.IsNotExist(err) {
		hooksDir = hooksSubDir
	}
	hooksRelativePaths, err := utils_file.RecursiveGetExecutablePaths(hooksDir)
	if err != nil {
		return nil, err
	}

	hooks = make([]*GlobalHook, 0)

	// sort hooks by path
	sort.Strings(hooksRelativePaths)
	log.Debugf("  Hook paths: %+v", hooksRelativePaths)

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(hooksDir, hookPath)
		if err != nil {
			return nil, err
		}

		globalHook := NewGlobalHook(hookName, hookPath)

		hooks = append(hooks, globalHook)
	}

	log.Infof("Registered %d global shell hooks from '%s'", len(hooks), hooksDir)

	return
}

func SearchGlobalGoHooks() (hooks []*GlobalHook, err error) {
	// find global hooks in go hooks registry
	hooks = make([]*GlobalHook, 0)
	goHooks := sdk.Registry().Hooks()
	for _, h := range goHooks {
		m := h.Metadata
		if !m.Global {
			continue
		}

		globalHook := NewGlobalHook(m.Name, m.Path)
		globalHook.WithGoHook(h.Hook)
		hooks = append(hooks, globalHook)
	}

	log.Infof("Registered %d global Go hooks", len(hooks))

	return hooks, nil
}

func SearchModuleHooks(module *Module) (hooks []*ModuleHook, err error) {
	hooks = make([]*ModuleHook, 0)

	shellHooks, err := SearchModuleShellHooks(module)
	if err != nil {
		return nil, err
	}
	hooks = append(hooks, shellHooks...)

	goHooks, err := SearchModuleGoHooks(module)
	if err != nil {
		return nil, err
	}
	hooks = append(hooks, goHooks...)

	sort.SliceStable(hooks, func(i, j int) bool {
		return hooks[i].Path < hooks[j].Path
	})

	return hooks, nil
}

func SearchModuleShellHooks(module *Module) (hooks []*ModuleHook, err error) {
	hooksDir := filepath.Join(module.Path, "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
	}

	hooksRelativePaths, err := utils_file.RecursiveGetExecutablePaths(hooksDir)
	if err != nil {
		return nil, err
	}

	hooks = make([]*ModuleHook, 0)

	// sort hooks by path
	sort.Strings(hooksRelativePaths)
	log.Debugf("  Hook paths: %+v", hooksRelativePaths)

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(filepath.Dir(module.Path), hookPath)
		if err != nil {
			return nil, err
		}

		moduleHook := NewModuleHook(hookName, hookPath)
		moduleHook.WithModule(module)

		hooks = append(hooks, moduleHook)
	}

	return
}

func SearchModuleGoHooks(module *Module) (hooks []*ModuleHook, err error) {
	// find module hooks in go hooks registry
	hooks = make([]*ModuleHook, 0)
	goHooks := sdk.Registry().Hooks()
	for _, h := range goHooks {
		m := h.Metadata
		if !m.Module {
			continue
		}
		if m.ModuleName != module.Name {
			continue
		}

		moduleHook := NewModuleHook(m.Name, m.Path)
		moduleHook.WithModule(module)
		moduleHook.WithGoHook(h.Hook)

		hooks = append(hooks, moduleHook)
	}

	return hooks, nil
}

func (mm *moduleManager) RegisterGlobalHooks() error {
	log.Debug("Search and register global hooks")

	mm.globalHooksOrder = make(map[sh_op_types.BindingType][]*GlobalHook)
	mm.globalHooksByName = make(map[string]*GlobalHook)

	hooks, err := SearchGlobalHooks(mm.GlobalHooksDir)
	if err != nil {
		return err
	}
	log.Debugf("Found %d global hooks", len(hooks))

	for _, globalHook := range hooks {
		logEntry := log.WithField("hook", globalHook.Name).
			WithField("hook.type", "global")

		var yamlConfigBytes []byte
		var goConfig *go_hook.HookConfig

		if globalHook.GoHook != nil {
			goConfig = globalHook.GoHook.Config()
		} else {
			yamlConfigBytes, err = NewHookExecutor(globalHook, nil, "", mm.KubeObjectPatcher).Config()
			if err != nil {
				logEntry.Errorf("Run --config: %s", err)
				return fmt.Errorf("global hook --config run problem")
			}
		}

		if len(yamlConfigBytes) > 0 {
			err = globalHook.WithConfig(yamlConfigBytes)
			if err != nil {
				logEntry.Errorf("Hook return bad config: %s", err)
				return fmt.Errorf("global hook return bad config")
			}
		} else {
			if goConfig != nil {
				err := globalHook.WithGoConfig(goConfig)
				if err != nil {
					logEntry.Errorf("Hook return bad config: %s", err)
					return fmt.Errorf("global hook return bad config")
				}
			}
		}

		globalHook.WithModuleManager(mm)

		// Add hook info as log labels
		for _, kubeCfg := range globalHook.Config.OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = globalHook.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "global"
			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
				"hook":    globalHook.Name,
				"binding": kubeCfg.BindingName,
				"module":  "", // empty "module" label for label set consistency with module hooks
				"queue":   kubeCfg.Queue,
				"kind":    kubeCfg.Monitor.Kind,
			}
		}

		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(globalHook.Hook.Config.OnKubernetesEvents, mm.kubeEventsManager)
		hookCtrl.InitScheduleBindings(globalHook.Config.Schedules, mm.scheduleManager)

		globalHook.WithHookController(hookCtrl)
		globalHook.WithTmpDir(mm.TempDir)

		// register global hook in indexes
		for _, binding := range globalHook.Config.Bindings() {
			mm.globalHooksOrder[binding] = append(mm.globalHooksOrder[binding], globalHook)
		}
		mm.globalHooksByName[globalHook.Name] = globalHook

		logEntry.Infof("Global hook '%s' successfully run with --config. Register with bindings: %s", globalHook.Name, globalHook.GetConfigDescription())

		mm.metricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(globalHook.Config.BindingsCount()),
			map[string]string{
				"hook":   globalHook.Name,
				"module": "", // empty "module" label for label set consistency with module hooks
			})
	}

	// Load validation schemas
	openApiDir := filepath.Join(mm.GlobalHooksDir, "openapi")
	configBytes, valuesBytes, err := ReadOpenAPIFiles(openApiDir)
	if err != nil {
		return fmt.Errorf("read global openAPI schemas: %v", err)
	}

	err = mm.ValuesValidator.SchemaStorage.AddGlobalValuesSchemas(configBytes, valuesBytes)
	if err != nil {
		return fmt.Errorf("add global schemas: %v", err)
	}

	return nil
}

func (mm *moduleManager) RegisterModuleHooks(module *Module, logLabels map[string]string) error {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels)).WithField("module", module.Name)

	if _, ok := mm.modulesHooksOrderByName[module.Name]; ok {
		logEntry.Debugf("Module hooks already registered")
		return nil
	}
	logEntry.Debugf("Search and register hooks")

	var registeredModuleHooks = make(map[sh_op_types.BindingType][]*ModuleHook)

	hooks, err := SearchModuleHooks(module)
	if err != nil {
		logEntry.Errorf("Search module hooks: %s", err)
		return err
	}
	logEntry.Debugf("Found %d hooks", len(hooks))

	for _, moduleHook := range hooks {
		hookLogEntry := logEntry.WithField("hook", moduleHook.Name).
			WithField("hook.type", "module")

		var yamlConfigBytes []byte
		var goConfig *go_hook.HookConfig

		if moduleHook.GoHook != nil {
			goConfig = moduleHook.GoHook.Config()
		} else {
			yamlConfigBytes, err = NewHookExecutor(moduleHook, nil, "", mm.KubeObjectPatcher).Config()
			if err != nil {
				hookLogEntry.Errorf("Run --config: %s", err)
				return fmt.Errorf("module hook --config run problem")
			}
		}

		if len(yamlConfigBytes) > 0 {
			err = moduleHook.WithConfig(yamlConfigBytes)
			if err != nil {
				hookLogEntry.Errorf("Hook return bad config: %s", err)
				return fmt.Errorf("module hook return bad config")
			}
		} else {
			if moduleHook.GoHook != nil {
				err := moduleHook.WithGoConfig(goConfig)
				if err != nil {
					logEntry.Errorf("Hook return bad config: %s", err)
					return fmt.Errorf("module hook return bad config")
				}
			}
		}

		moduleHook.WithModuleManager(mm)

		// Add hook info as log labels
		for _, kubeCfg := range moduleHook.Config.OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["module"] = module.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = moduleHook.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "module"
			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
				"hook":    moduleHook.Name,
				"binding": kubeCfg.BindingName,
				"module":  module.Name,
				"queue":   kubeCfg.Queue,
				"kind":    kubeCfg.Monitor.Kind,
			}
		}

		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(moduleHook.Config.OnKubernetesEvents, mm.kubeEventsManager)
		hookCtrl.InitScheduleBindings(moduleHook.Config.Schedules, mm.scheduleManager)

		moduleHook.WithHookController(hookCtrl)
		moduleHook.WithTmpDir(mm.TempDir)

		// register module hook in indexes
		for _, binding := range moduleHook.Config.Bindings() {
			registeredModuleHooks[binding] = append(registeredModuleHooks[binding], moduleHook)
		}

		hookLogEntry.Infof("Module hook successfully run with --config. Register with bindings: %s", moduleHook.GetConfigDescription())

		mm.metricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(moduleHook.Config.BindingsCount()),
			map[string]string{
				"module": module.Name,
				"hook":   moduleHook.Name,
			})
	}

	// Save registered hooks in mm.modulesHooksOrderByName
	if mm.modulesHooksOrderByName[module.Name] == nil {
		mm.modulesHooksOrderByName[module.Name] = make(map[sh_op_types.BindingType][]*ModuleHook)
	}
	mm.modulesHooksOrderByName[module.Name] = registeredModuleHooks

	return nil
}
