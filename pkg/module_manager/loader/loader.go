package loader

import (
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
)

type ModuleLoader interface {
	LoadModules() ([]*modules.BasicModule, error)
	LoadModule(moduleSource string, modulePath string) (*modules.BasicModule, error)
}

// ModuleVersion represents a module's name and its deployed version.
type ModuleVersion struct {
	Name    string
	Version string
}

// ModuleTelemetryProvider is an optional interface that ModuleLoader implementations
// can satisfy to expose deployed module versions for telemetry collection.
type ModuleTelemetryProvider interface {
	ModuleTelemetry() []ModuleVersion
}
