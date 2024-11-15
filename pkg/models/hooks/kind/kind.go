package kind

import (
	gohook "github.com/flant/addon-operator/pkg/module_manager/go-hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
	metric_operation "github.com/flant/shell-operator/pkg/metric-storage/operation"
	objectpatch "github.com/flant/shell-operator/pkg/object-patch"
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
	Metrics                 []metric_operation.MetricOperation
	ObjectPatcherOperations []objectpatch.Operation
	BindingActions          []gohook.BindingAction
}
