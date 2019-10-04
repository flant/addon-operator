package module_manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kennygrant/sanitize"
	"github.com/romana/rlog"

	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

type Hook interface {
	WithModuleManager(moduleManager *MainModuleManager)
	WithConfig(configOutput []byte) (err error)
	GetName() string
	GetPath() string
	PrepareTmpFilesForHookRun(context interface{}) (map[string]string, error)
	Order(binding BindingType) float64
}

type CommonHook struct {
	// The unique name like 'global-hooks/startup_hook' or '002-module/hooks/cleanup'.
	Name           string
	// The absolute path of the executable file.
	Path           string

	moduleManager *MainModuleManager
}

func (c *CommonHook) WithModuleManager(moduleManager *MainModuleManager) {
	c.moduleManager = moduleManager
}

func (h *CommonHook) SafeName() string {
	return sanitize.BaseName(h.Name)
}

func (h *CommonHook) GetName() string {
	return h.Name
}

func (h *CommonHook) GetPath() string {
	return h.Path
}


func SearchGlobalHooks(hooksDir string) (hooks []*GlobalHook, err error) {
	rlog.Debug("INIT: search global hooks...")

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
	rlog.Debugf("  Hook paths: %+v", hooksRelativePaths)

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(hooksDir, hookPath)
		if err != nil {
			return nil, err
		}

		rlog.Infof("INIT: global hook '%s'", hookName)

		globalHook := NewGlobalHook(hookName, hookPath)

		hooks = append(hooks, globalHook)
	}

	return
}

func SearchModuleHooks(module *Module) (hooks []*ModuleHook, err error) {
	rlog.Infof("INIT: module '%s' hooks ...", module.Name)

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
	rlog.Debugf("  Hook paths: %+v", hooksRelativePaths)

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(filepath.Dir(module.Path), hookPath)
		if err != nil {
			return nil, err
		}

		rlog.Infof("INIT:   hook '%s' ...", hookName)

		moduleHook := NewModuleHook(hookName, hookPath)
		moduleHook.WithModule(module)

		hooks = append(hooks, moduleHook)
	}

	return
}


func LoadModuleHooksConfig(hooks []*ModuleHook) error {
	for _, hook := range hooks {
		configOutput, err := NewHookExecutor(hook, nil).Config()
		if err != nil {
			return fmt.Errorf("hook '%s' config: %s", hook.GetPath(), err)
		}

		err = hook.WithConfig(configOutput)
		if err != nil {
			return fmt.Errorf("creating global hook '%s': %s", hook.GetName(), err)
		}
	}

	return nil
}

func LoadGlobalHooksConfig(hooks []*GlobalHook) error {
	for _, hook := range hooks {
		configOutput, err := NewHookExecutor(hook, nil).Config()
		if err != nil {
			return fmt.Errorf("hook '%s' config: %s", hook.GetPath(), err)
		}

		err = hook.WithConfig(configOutput)
		if err != nil {
			return fmt.Errorf("creating module hook '%s': %s", hook.GetName(), err)
		}
	}

	return nil
}


func (mm *MainModuleManager) RegisterGlobalHooks() error {
	rlog.Debug("INIT: global hooks")

	mm.globalHooksOrder = make(map[BindingType][]*GlobalHook)
	mm.globalHooksByName = make(map[string]*GlobalHook)

	hooks, err := SearchGlobalHooks(mm.GlobalHooksDir)
	if err != nil {
		return err
	}

	err = LoadGlobalHooksConfig(hooks)
	if err != nil {
		return err
	}

	for _, globalHook := range hooks {
		globalHook.WithModuleManager(mm)
		// register global hook in indexes
		for _, binding := range globalHook.Config.Bindings() {
			mm.globalHooksOrder[binding] = append(mm.globalHooksOrder[binding], globalHook)
		}
		mm.globalHooksByName[globalHook.Name] = globalHook
	}

	return nil
}

func (mm *MainModuleManager) RegisterModuleHooks(module *Module) error {
	if _, ok := mm.modulesHooksOrderByName[module.Name]; ok {
		rlog.Debugf("INIT: module '%s' hooks: already initialized", module.Name)
		return nil
	}

	rlog.Infof("INIT: module '%s' hooks ...", module.Name)

	hooks, err := SearchModuleHooks(module)
	if err != nil {
		return err
	}

	err = LoadModuleHooksConfig(hooks)
	if err != nil {
		return err
	}

	for _, moduleHook := range hooks {
		moduleHook.WithModuleManager(mm)
		// register module hook in indexes
		for _, binding := range moduleHook.Config.Bindings() {
			if mm.modulesHooksOrderByName[module.Name] == nil {
				mm.modulesHooksOrderByName[module.Name] = make(map[BindingType][]*ModuleHook)
			}
			mm.modulesHooksOrderByName[module.Name][binding] = append(mm.modulesHooksOrderByName[module.Name][binding], moduleHook)
		}
	}

	return nil
}
