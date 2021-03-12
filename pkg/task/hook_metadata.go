package task

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"strings"

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
	Binding          string // binding name from configuration
	BindingType      BindingType
	BindingContext   []BindingContext
	AllowFailure     bool //Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.

	OnStartupHooks bool // Execute onStartup and kubernetes@Synchronization hooks for module

	ValuesChecksum           string // checksum of global values before first afterAll hook execution
	DynamicEnabledChecksum   string // checksum of dynamicEnabled before first afterAll hook execution
	LastAfterAllHook         bool   // true if task is a last afterAll hook in sequence
	ReloadAllOnValuesChanges bool   // whether or not run DiscoverModules process if hook change global values

	KubernetesBindingId      string   // Unique id for kubernetes bindings
	WaitForSynchronization   bool     // kubernetes.Synchronization task should be waited
	MonitorIDs               []string // an array of monitor IDs to unlock Kubernetes events after Synchronization.
	ExecuteOnSynchronization bool     // A flag to skip hook execution in Synchronization tasks.
}

var _ task_metadata.HookNameAccessor = HookMetadata{}
var _ task_metadata.BindingContextAccessor = HookMetadata{}
var _ task_metadata.MonitorIDAccessor = HookMetadata{}
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
	bindings := []string{}
	for _, bc := range hm.BindingContext {
		bindings = append(bindings, bc.Binding)
	}
	bindingNames := ""
	if len(bindings) > 0 {
		bindingNames = ":" + strings.Join(bindings, ",")
	}

	if hm.ModuleName == "" {
		// global hook
		return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
	} else {
		if hm.HookName == "" {
			// module run
			osh := ""
			if hm.OnStartupHooks {
				osh = ":onStartupHooks"
			}
			return fmt.Sprintf("%s%s%s:%s", hm.ModuleName, osh, bindingNames, hm.EventDescription)
		} else {
			// module hook
			return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
		}
	}
}

func (hm HookMetadata) GetHookName() string {
	return hm.HookName
}

func (hm HookMetadata) GetBindingContext() []BindingContext {
	return hm.BindingContext
}

func (hm HookMetadata) GetMonitorIDs() []string {
	return hm.MonitorIDs
}
