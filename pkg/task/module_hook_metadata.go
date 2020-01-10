package task

import (
	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/task"
)

// HookMetadata is metadata for addon-operator tasks
type HookMetadata struct {
	HookName string
	ModuleName string
	BindingType BindingType
	BindingContext []BindingContext
	AllowFailure   bool //Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.

	OnStartupHooks bool
}

func HookMetadataAccessor(t task.Task) (meta HookMetadata) {
	taskMeta := t.GetMetadata()
	if taskMeta == nil {
		log.Errorf("Possible Bug! task metadata is nil")
		return
	}
	meta, ok := taskMeta.(HookMetadata)
	if !ok {
		log.Errorf("Possible Bug! task '%s' metadata is not of type ModuleHookMetadata: got %T", t.GetType(), meta)
		return
	}
	return
}

