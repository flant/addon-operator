package module_manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/types"

	"github.com/flant/shell-operator/pkg/hook"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

type Hook interface {
	WithModuleManager(moduleManager *moduleManager)
	WithConfig(configOutput []byte) (err error)
	WithHookController(hookController controller.HookController)
	GetHookController() controller.HookController
	GetName() string
	GetPath() string
	PrepareTmpFilesForHookRun(bindingContext []byte) (map[string]string, error)
	Order(binding BindingType) float64
}

type CommonHook struct {
	hook.Hook

	moduleManager *moduleManager
}

func (c *CommonHook) WithModuleManager(moduleManager *moduleManager) {
	c.moduleManager = moduleManager
}

func (h *CommonHook) GetName() string {
	return h.Name
}

func (h *CommonHook) GetPath() string {
	return h.Path
}

// SearchGlobalHooks recursively find all executables in hooksDir. Absent hooksDir is not an error.
func SearchGlobalHooks(hooksDir string) (hooks []*GlobalHook, err error) {
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
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

	return
}

func SearchModuleHooks(module *Module) (hooks []*ModuleHook, err error) {
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

func (mm *moduleManager) RegisterGlobalHooks() error {
	log.Debug("Search and register global hooks")

	mm.globalHooksOrder = make(map[BindingType][]*GlobalHook)
	mm.globalHooksByName = make(map[string]*GlobalHook)

	hooks, err := SearchGlobalHooks(mm.GlobalHooksDir)
	if err != nil {
		return err
	}
	log.Debugf("Found %d global hooks", len(hooks))

	for _, globalHook := range hooks {
		logEntry := log.WithField("hook", globalHook.Name).
			WithField("hook.type", "global")

		configOutput, err := NewHookExecutor(globalHook, nil).Config()
		if err != nil {
			logEntry.Errorf("Run --config: %s", err)
			return fmt.Errorf("global hook --config run problem")
		}

		err = globalHook.WithConfig(configOutput)
		if err != nil {
			logEntry.Errorf("Hook return bad config: %s", err)
			return fmt.Errorf("global hook return bad config")
		}

		globalHook.WithModuleManager(mm)

		// Add hook info as log labels
		for _, kubeCfg := range globalHook.Config.OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = globalHook.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "global"
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

		logEntry.Infof("Global hook successfully run with --config. Register with bindings: %s", globalHook.GetConfigDescription())
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

	hooks, err := SearchModuleHooks(module)
	if err != nil {
		logEntry.Errorf("Search module hooks: %s", err)
		return err
	}
	logEntry.Debugf("Found %d hooks", len(hooks))

	for _, moduleHook := range hooks {
		hookLogEntry := logEntry.WithField("hook", moduleHook.Name).
			WithField("hook.type", "module")

		configOutput, err := NewHookExecutor(moduleHook, nil).Config()
		if err != nil {
			hookLogEntry.Errorf("Run --config: %s", err)
			return fmt.Errorf("module hook --config run problem")
		}

		err = moduleHook.WithConfig(configOutput)
		if err != nil {
			hookLogEntry.Errorf("Hook return bad config: %s", err)
			return fmt.Errorf("module hook return bad config")
		}

		moduleHook.WithModuleManager(mm)

		// Add hook info as log labels
		for _, kubeCfg := range moduleHook.Config.OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["module"] = module.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = moduleHook.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "module"
		}

		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(moduleHook.Config.OnKubernetesEvents, mm.kubeEventsManager)
		hookCtrl.InitScheduleBindings(moduleHook.Config.Schedules, mm.scheduleManager)

		moduleHook.WithHookController(hookCtrl)
		moduleHook.WithTmpDir(mm.TempDir)

		// register module hook in indexes
		for _, binding := range moduleHook.Config.Bindings() {
			if mm.modulesHooksOrderByName[module.Name] == nil {
				mm.modulesHooksOrderByName[module.Name] = make(map[BindingType][]*ModuleHook)
			}
			mm.modulesHooksOrderByName[module.Name][binding] = append(mm.modulesHooksOrderByName[module.Name][binding], moduleHook)
		}

		hookLogEntry.Infof("Module hook successfully run with --config. Register with bindings: %s", moduleHook.GetConfigDescription())
	}

	return nil
}
