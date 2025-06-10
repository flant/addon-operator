package system

import (
	"k8s.io/utils/ptr"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	exterr "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/error"
)

const (
	Name extenders.ExtenderName = "System"
)

type Extender struct {
	// check if the cluster bootstrapped
	isBootstrapped func() (bool, error)
	modules        map[string]bool
}

func NewExtender(helper func() (bool, error)) *Extender {
	return &Extender{
		modules:        make(map[string]bool),
		isBootstrapped: helper,
	}
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(moduleName string, _ map[string]string) (*bool, error) {
	if system, ok := e.modules[moduleName]; ok {
		// system modules do not require bootstrapped cluster
		if system {
			return ptr.To(true), nil
		}

		bootstrapped, err := e.isBootstrapped()
		if err != nil {
			return nil, exterr.Permanent(err)
		}

		// enable functional modules only if the cluster bootstrapped
		return ptr.To(bootstrapped), nil
	}

	return nil, nil
}

func (e *Extender) IsTerminator() bool {
	return true
}

func (e *Extender) AddBasicModule(moduleName string, system bool) {
	e.modules[moduleName] = system
}
