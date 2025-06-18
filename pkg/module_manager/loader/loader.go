package loader

import (
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
)

type ModuleLoader interface {
	LoadModules() ([]*modules.BasicModule, error)
	LoadModule(modulePath string) (*modules.BasicModule, error)
}
