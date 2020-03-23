package task

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/task_metadata"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/task"
)

// HookMetadata is metadata for addon-operator tasks
type HookMetadata struct {
	EventDescription string // event name for informative queue dump
	HookName         string
	ModuleName       string
	BindingType      BindingType
	BindingContext   []BindingContext
	AllowFailure     bool //Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.

	OnStartupHooks bool // Execute onStartup and kubernetes@Synchronization hooks for module

	LastAfterAllHook         bool   // True if task is a last hook in afterAll sequence
	ValuesChecksum           string // checksum of global values between first afterAll hook execution
	ReloadAllOnValuesChanges bool   // whether or not run DiscoverModules process if hook change global values
}

var _ task_metadata.HookNameAccessor = HookMetadata{}
var _ task_metadata.BindingContextAccessor = HookMetadata{}
var _ task.MetadataDescriptable = HookMetadata{}

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

func (hm HookMetadata) GetDescription() string {
	if hm.ModuleName == "" {
		// global hook
		return fmt.Sprintf("%s:%s:%s", string(hm.BindingType), hm.HookName, hm.EventDescription)
	} else {
		if hm.HookName == "" {
			// module run
			osh := ""
			if hm.OnStartupHooks {
				osh = ":onStartupHooks"
			}
			return fmt.Sprintf("%s%s:%s", hm.ModuleName, osh, hm.EventDescription)
		} else {
			// module hook
			return fmt.Sprintf("%s:%s:%s", string(hm.BindingType), hm.HookName, hm.EventDescription)
		}
	}
}

func (m HookMetadata) GetHookName() string {
	return m.HookName
}

func (m HookMetadata) GetBindingContext() []BindingContext {
	return m.BindingContext
}
