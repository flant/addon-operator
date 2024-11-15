package loader

import (
	"github.com/flant/addon-operator/pkg/models/modules"
)

type ModuleLoader interface {
	LoadModules() ([]*modules.BasicModule, error)
	LoadModule(moduleSource string, modulePath string) (*modules.BasicModule, error)
}
