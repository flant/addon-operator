package modules

import "github.com/deckhouse/deckhouse/pkg/log"

type moduleOption func(optionsApplier ModuleOptionApplier)

func (opt moduleOption) Apply(o ModuleOptionApplier) {
	opt(o)
}

func WithLogger(logger *log.Logger) moduleOption {
	return func(optionsApplier ModuleOptionApplier) {
		optionsApplier.WithLogger(logger)
	}
}

type ModuleOption interface {
	Apply(optsApplier ModuleOptionApplier)
}

type ModuleOptionApplier interface {
	WithLogger(logger *log.Logger)
}
