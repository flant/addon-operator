package script_enabled

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

type Extender struct {
	tmpDir string

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
		enabledModules: make([]string, 0),
		tmpDir:         tmpDir,
	}

	return e, nil
}

func (e *Extender) Dump() map[string]bool {
	return nil
}

func (e *Extender) Name() extenders.ExtenderName {
	return extenders.ScriptEnabledExtender
}

func (e *Extender) IsShutter() bool {
	return true
}

func (e *Extender) Reset() {
	e.l.Lock()
	e.enabledModules = make([]string, 0)
	e.l.Unlock()
}

func (e *Extender) Filter(module node.ModuleInterface) (*bool, error) {
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

func (e *Extender) IsNotifier() bool {
	return false
}

func (e *Extender) SetNotifyChannel(_ context.Context, _ chan extenders.ExtenderEvent) {
}

func (e *Extender) Order() {
}
