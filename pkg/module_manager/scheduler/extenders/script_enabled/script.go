package script_enabled

import (
	"fmt"
	"os"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

const (
	Name extenders.ExtenderName = "ScriptEnabled"
)

type Extender struct {
	tmpDir       string
	basicModules map[string]node.ModuleInterface

	l              sync.RWMutex
	enabledModules []string
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
		basicModules:   make(map[string]node.ModuleInterface),
		enabledModules: make([]string, 0),
		tmpDir:         tmpDir,
	}

	return e, nil
}

func (e *Extender) AddBasicModule(module node.ModuleInterface) {
	e.basicModules[module.GetName()] = module
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

func (e *Extender) Filter(moduleName string) (*bool, error) {
	if module, found := e.basicModules[moduleName]; found {
		enabled, err := module.RunEnabledScript(e.tmpDir, e.enabledModules, map[string]string{"operator.component": "ModuleManager.Scheduler", "extender": "script_enabled"})
		if err != nil {
			return nil, err
		}

		if enabled {
			e.l.Lock()
			e.enabledModules = append(e.enabledModules, module.GetName())
			e.l.Unlock()
		}
		return &enabled, nil
	}
	return nil, nil
}

func (e *Extender) IsNotifier() bool {
	return false
}

func (e *Extender) Order() {
}
