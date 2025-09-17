package kind

import (
	"github.com/deckhouse/deckhouse/pkg/metrics-storage/operation"
	sdkpkg "github.com/deckhouse/module-sdk/pkg"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
)

// HookKind kind of the hook
type HookKind string

var (
	// HookKindGo for go hooks
	HookKindGo HookKind = "go"
	// HookKindShell for shell hooks (bash, python, etc)
	HookKindShell HookKind = "shell"
)

// HookResult returns result of a hook execution
type HookResult struct {
	Usage                   *executor.CmdUsage
	Patches                 map[utils.ValuesPatchType]*utils.ValuesPatch
	Metrics                 []operation.MetricOperation
	ObjectPatcherOperations []sdkpkg.PatchCollectorOperation
	BindingActions          []gohook.BindingAction
}
