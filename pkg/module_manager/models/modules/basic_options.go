package modules

import "github.com/deckhouse/deckhouse/pkg/log"

type basicModuleOption func(optionsApplier BasicModuleOptionApplier)

func (opt basicModuleOption) Apply(o BasicModuleOptionApplier) {
	opt(o)
}

func WithLogger(logger *log.Logger) basicModuleOption {
	return func(optionsApplier BasicModuleOptionApplier) {
		optionsApplier.WithLogger(logger)
	}
}

type BasicModuleOption interface {
	Apply(optsApplier BasicModuleOptionApplier)
}

type BasicModuleOptionApplier interface {
	WithLogger(logger *log.Logger)
}
