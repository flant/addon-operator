package kind

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"
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
	ObjectPatcherOperations []object_patch.Operation
	BindingActions          []go_hook.BindingAction
}
