package script_enabled

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	"github.com/flant/addon-operator/pkg/utils"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

const (
	Name extenders.ExtenderName = "ScriptEnabled"

	noEnabledScript     scriptState = "NoEnabledScript"
	nonExecutableScript scriptState = "NonExecutableScript"
	statError           scriptState = "StatError"
)

type scriptState string

type Extender struct {
	tmpDir                 string
	basicModuleDescriptors map[string]moduleDescriptor

	l              sync.RWMutex
	enabledModules []string
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
		return nil, fmt.Errorf("%s path isn't a directory", tmpDir)
	}

	e := &Extender{
		basicModuleDescriptors: make(map[string]moduleDescriptor),
		enabledModules:         make([]string, 0),
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
			log.Debugf("MODULE '%s' is ENABLED. Enabled script doesn't exist!", module.GetName())
		} else {
			moduleD.scriptState = statError
			moduleD.stateDescription = fmt.Sprintf("Cannot stat enabled script for '%s' module: %v", module.GetName(), err)
			log.Errorf(moduleD.stateDescription)
		}
	} else {
		if !utils_file.IsFileExecutable(f) {
			moduleD.scriptState = nonExecutableScript
			log.Warnf("Found non-executable enabled script for '%s' module - assuming enabled state", module.GetName())
		}
	}
	e.basicModuleDescriptors[module.GetName()] = moduleD
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) IsShutter() bool {
	return true
}

func (e *Extender) Reset() {
	e.l.Lock()
	e.enabledModules = make([]string, 0)
	e.l.Unlock()
}

func (e *Extender) Filter(moduleName string, logLabels map[string]string) (*bool, error) {
	if moduleDescriptor, found := e.basicModuleDescriptors[moduleName]; found {
		var err error
		var enabled bool

		switch moduleDescriptor.scriptState {
		case "":
			refreshLogLabels := utils.MergeLabels(logLabels, map[string]string{
				"extender": "ScriptEnabled",
			})
			enabled, err = moduleDescriptor.module.RunEnabledScript(e.tmpDir, e.enabledModules, refreshLogLabels)
			if err != nil {
				err = fmt.Errorf("Failed to execute '%s' module's enabled script: %v", moduleDescriptor.module.GetName(), err)
			}

		case statError:
			log.Errorf(moduleDescriptor.stateDescription)
			enabled = false
			err = errors.New(moduleDescriptor.stateDescription)

		case nonExecutableScript:
			enabled = true
			log.Warnf("Found non-executable enabled script for '%s' module - assuming enabled state", moduleDescriptor.module.GetName())

		case noEnabledScript:
			enabled = true
			log.Debugf("MODULE '%s' is ENABLED. Enabled script doesn't exist!", moduleDescriptor.module.GetName())
		}

		if enabled {
			e.l.Lock()
			e.enabledModules = append(e.enabledModules, moduleDescriptor.module.GetName())
			e.l.Unlock()
		}
		return &enabled, err
	}
	return nil, nil
}

func (e *Extender) Order() {
}
