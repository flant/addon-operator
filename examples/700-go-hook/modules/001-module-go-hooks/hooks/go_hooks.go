package hooks

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

var _ = sdk.RegisterFunc(&go_hook.HookConfig{
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
			FilterFunc:                   ObjFilter,
			ExecuteHookOnSynchronization: go_hook.Bool(true),
		},
	},

	Schedule: []go_hook.ScheduleConfig{
		{
			Name:    "metrics",
			Crontab: "*/5 * * * * *",
		},
	},
}, run)

type podSpecFilteredObj v1.PodSpec

func ObjFilter(obj *unstructured.Unstructured) (go_hook.FilterResult, error) {
	pod := &v1.Pod{}
	err := sdk.FromUnstructured(obj, pod)
	if err != nil {
		return nil, err
	}

	podSpec := pod.Spec

	return &podSpec, nil
}

func run(input *go_hook.HookInput) error {
	for _, o := range input.Snapshots["pods"] {
		podSpec := o.(*podSpecFilteredObj)
		input.Logger.Infof("Got podSpec: %+v", podSpec)
	}

	input.Logger.Infof("Hello from on_kube.pods2! I have %d snapshots\n",
		len(input.Snapshots))

	input.MetricsCollector.Add("addon_go_hooks_total", 1.0, nil)

	input.ConfigValues.Set("moduleGoHooks.time", time.Now().Unix())
	input.Values.Set("moduleGoHooks.timeTemp", time.Now().Unix())

	return nil
}
