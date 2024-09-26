package task

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/task_metadata"
	"github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/task"

	log "github.com/sirupsen/logrus"
)

// HookMetadata is a metadata for addon-operator tasks
type HookMetadata struct {
	EventDescription string // event name for informative queue dump
	HookName         string
	ModuleName       string
	Binding          string // binding name from configuration
	BindingType      types.BindingType
	BindingContext   []binding_context.BindingContext
	AllowFailure     bool // Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.

	DoModuleStartup bool // Execute onStartup and kubernetes@Synchronization hooks for module
	IsReloadAll     bool // ModuleRun task is a part of 'Reload all modules' process.

	ValuesChecksum           string // checksum of global values before first afterAll hook execution
	GlobalValuesChanged      bool   // global values changed flag
	LastAfterAllHook         bool   // true if task is a last afterAll hook in sequence
	ReloadAllOnValuesChanges bool   // whether to run DiscoverModules process if hook change global values

	KubernetesBindingId      string   // Unique id for kubernetes bindings
	WaitForSynchronization   bool     // kubernetes.Synchronization task should be waited
	MonitorIDs               []string // an array of monitor IDs to unlock Kubernetes events after Synchronization.
	ExecuteOnSynchronization bool     // A flag to skip hook execution in Synchronization tasks.
}

var (
	_ task_metadata.HookNameAccessor       = HookMetadata{}
	_ task_metadata.BindingContextAccessor = HookMetadata{}
	_ task_metadata.MonitorIDAccessor      = HookMetadata{}
	_ task.MetadataDescriptionGetter       = HookMetadata{}
)

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
	bindingsMap := make(map[string]struct{})
	bindings := make([]string, 0)

	for _, bc := range hm.BindingContext {
		bindingsMap[bc.Binding] = struct{}{}
	}
	for bindingName := range bindingsMap {
		bindings = append(bindings, bindingName)
	}
	sort.Strings(bindings)
	bindingNames := ""
	if len(bindings) > 0 {
		bindingNames = ":" + strings.Join(bindings, ",")
	}
	if len(hm.BindingContext) > 1 {
		bindingNames = fmt.Sprintf("%s in %d contexts", bindingNames, len(hm.BindingContext))
	}

	if hm.ModuleName == "" {
		// global hook
		return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
	}

	if hm.HookName == "" {
		// module run
		osh := ""
		if hm.DoModuleStartup {
			osh = ":doStartup"
		}
		return fmt.Sprintf("%s%s%s:%s", hm.ModuleName, osh, bindingNames, hm.EventDescription)
	}

	// module hook
	return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
}

func (hm HookMetadata) GetHookName() string {
	return hm.HookName
}

func (hm HookMetadata) GetBindingContext() []binding_context.BindingContext {
	return hm.BindingContext
}

func (hm HookMetadata) GetMonitorIDs() []string {
	return hm.MonitorIDs
}

func (hm HookMetadata) IsSynchronization() bool {
	return len(hm.BindingContext) > 0 && hm.BindingContext[0].IsSynchronization()
}
