package go_hooks

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

const moduleName = "node_manager"

var _ = sdk.RegisterFunc(&go_hook.HookConfig{
	Queue: fmt.Sprintf("/modules/%s/handle_node_templates", moduleName),
	Schedule: []go_hook.ScheduleConfig{
		{
			Name:    "cron",
			Crontab: "*/10 * * * * *",
		},
	},
	Kubernetes: []go_hook.KubernetesConfig{
		{
			Name:                         "ngs",
			ApiVersion:                   "",
			Kind:                         "",
			NameSelector:                 nil,
			NamespaceSelector:            nil,
			LabelSelector:                nil,
			FieldSelector:                nil,
			ExecuteHookOnSynchronization: go_hook.Bool(false),
			WaitForSynchronization:       go_hook.Bool(false),
			Filterable:                   nil,
		},
	},
	OnStartup:         &go_hook.OrderedConfig{Order: 20},
	OnBeforeHelm:      &go_hook.OrderedConfig{Order: 20},
	OnAfterHelm:       &go_hook.OrderedConfig{Order: 20},
	OnAfterDeleteHelm: &go_hook.OrderedConfig{Order: 20},
	OnBeforeAll:       &go_hook.OrderedConfig{Order: 20},
	OnAfterAll:        &go_hook.OrderedConfig{Order: 20},
}, Main)

func Main(input *go_hook.HookInput) error {
	return nil
}
