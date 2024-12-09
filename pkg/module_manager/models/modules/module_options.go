package modules

import "github.com/deckhouse/deckhouse/pkg/log"

type Option func(optionsApplier ModuleOptionApplier)

func (opt Option) Apply(o ModuleOptionApplier) {
	opt(o)
}

func WithLogger(logger *log.Logger) Option {
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
