package hooks

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"

	"github.com/flant/shell-operator/pkg/metric_storage/operation"
)

var _ = sdk.Register(&GoHook{})

type podSpecFilteredObj v1.PodSpec

func (ps *podSpecFilteredObj) ApplyFilter(obj *unstructured.Unstructured) (go_hook.FilterResult, error) {
	pod := &v1.Pod{}
	err := go_hook.ConvertUnstructured(obj, pod)
	if err != nil {
		return nil, err
	}

	podSpec := pod.Spec

	return &podSpec, nil
}

type GoHook struct{}

func (h *GoHook) Config() *go_hook.HookConfig {
	return &go_hook.HookConfig{
		OnStartup: &go_hook.OrderedConfig{
			Order: 10,
		},

		OnBeforeHelm: &go_hook.OrderedConfig{
			Order: 10,
		},

		Kubernetes: []go_hook.KubernetesConfig{
			{
				Name:                         "pods",
				ApiVersion:                   "v1",
				Kind:                         "Pods",
				Filterable:                   &podSpecFilteredObj{},
				ExecuteHookOnSynchronization: go_hook.Bool(true),
			},
		},

		Schedule: []go_hook.ScheduleConfig{
			{
				Name:    "metrics",
				Crontab: "*/5 * * * * *",
			},
		},
	}
}

func (h *GoHook) Metadata() *go_hook.HookMetadata {
	return &go_hook.HookMetadata{
		Name:       "go_hook.go",
		Path:       "001-module-go-hooks/hooks/go_hook.go",
		Module:     true,
		ModuleName: "module-go-hooks",
	}
}

func (h *GoHook) Run(input *go_hook.HookInput) error {
	for _, o := range input.Snapshots["pods"] {
		podSpec := o.(*podSpecFilteredObj)
		input.LogEntry.Infof("Got podSpec: %+v", podSpec)
	}

	input.LogEntry.Infof("Hello from on_kube.pods2! I have %d snapshots\n",
		len(input.Snapshots))

	v := 1.0
	*input.Metrics = append(*input.Metrics, operation.MetricOperation{
		Name: "addon_go_hooks_total",
		Add:  &v,
	})

	input.ConfigValues.Set("moduleGoHooks.time", time.Now().Unix())
	input.Values.Set("moduleGoHooks.timeTemp", time.Now().Unix())

	return nil
}
