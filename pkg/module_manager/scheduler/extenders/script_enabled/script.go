package script_enabled

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/deckhouse/deckhouse/pkg/log"
	pointer "k8s.io/utils/ptr"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	exerror "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/error"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	"github.com/flant/addon-operator/pkg/utils"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

type scriptState string

const (
	Name extenders.ExtenderName = "ScriptEnabled"

	noEnabledScript     scriptState = "NoEnabledScript"
	nonExecutableScript scriptState = "NonExecutableScript"
	statError           scriptState = "StatError"
)

type Extender struct {
	tmpDir                 string
	basicModuleDescriptors map[string]moduleDescriptor
	modulesStateHelper     func() []string
}

type moduleDescriptor struct {
	module           node.ModuleInterface
	scriptState      scriptState
	stateDescription string
}

func NewExtender(tmpDir string) (*Extender, error) {
	info, err := os.Stat(tmpDir)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("%q path isn't a directory", tmpDir)
	}

	e := &Extender{
		basicModuleDescriptors: make(map[string]moduleDescriptor),
		tmpDir:                 tmpDir,
	}

	return e, nil
}

func (e *Extender) AddBasicModule(module node.ModuleInterface) {
	moduleD := moduleDescriptor{
		module: module,
	}

	enabledScriptPath := filepath.Join(module.GetPath(), "enabled")
	f, err := os.Stat(enabledScriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			moduleD.scriptState = noEnabledScript
			log.Debug("MODULE is ENABLED. Enabled script doesn't exist!",
				slog.String("module", module.GetName()))
		} else {
			moduleD.scriptState = statError
			moduleD.stateDescription = fmt.Sprintf("Cannot stat enabled script for '%s' module: %v", module.GetName(), err)
			log.Error(moduleD.stateDescription)
		}
	} else {
		if utils_file.CheckExecutablePermissions(f) != nil {
			moduleD.scriptState = nonExecutableScript
			log.Warn("Found non-executable enabled script for module - assuming enabled state",
				slog.String("module", module.GetName()))
		}
	}

	e.basicModuleDescriptors[module.GetName()] = moduleD
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(moduleName string, logLabels map[string]string) (*bool, error) {
	if moduleDescriptor, found := e.basicModuleDescriptors[moduleName]; found {
		var err error
		var enabled *bool

		switch moduleDescriptor.scriptState {
		case "":
			var isEnabled bool
			refreshLogLabels := utils.MergeLabels(logLabels, map[string]string{
				"extender": "ScriptEnabled",
			})
			isEnabled, err = moduleDescriptor.module.RunEnabledScript(context.Background(), e.tmpDir, e.GetEnabledModules(), refreshLogLabels)
			if err != nil {
				err = fmt.Errorf("failed to execute '%s' module's enabled script: %v", moduleDescriptor.module.GetName(), err)
			}
			enabled = &isEnabled

		case statError:
			log.Error(moduleDescriptor.stateDescription)
			enabled = pointer.To(false)
			err = errors.New(moduleDescriptor.stateDescription)

		case nonExecutableScript:
			log.Warn("Found non-executable enabled script for module - assuming enabled state",
				slog.String("module", moduleDescriptor.module.GetName()))

		case noEnabledScript:
			log.Debug("MODULE is ENABLED. Enabled script doesn't exist!",
				slog.String("module", moduleDescriptor.module.GetName()))
		}

		if err != nil {
			return enabled, exerror.Permanent(err)
		}

		return enabled, nil
	}

	return nil, nil
}

func (e *Extender) IsTerminator() bool {
	return true
}

func (e *Extender) SetModulesStateHelper(f func() []string) {
	e.modulesStateHelper = f
}

func (e *Extender) GetEnabledModules() []string {
	if e.modulesStateHelper == nil {
		return nil
	}

	return e.modulesStateHelper()
}
