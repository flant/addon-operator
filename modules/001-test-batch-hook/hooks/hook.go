package hooks

import (
	"fmt"

	sdkobjectpatch "github.com/deckhouse/module-sdk/pkg/object-patch"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	v1core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = sdk.RegisterFunc(&go_hook.HookConfig{
	Kubernetes: []go_hook.KubernetesConfig{
		{
			Name:       "test_hook",
			ApiVersion: "v1",
			Kind:       "Pod",
			FilterFunc: applyTestHookFilter,
		},
	},
}, runTestHook)

func applyTestHookFilter(obj *unstructured.Unstructured) (go_hook.FilterResult, error) {
	pod := v1core.Pod{}
	err := sdk.FromUnstructured(obj, &pod)
	if err != nil {
		return nil, err
	}
	return pod, nil
}

func runTestHook(input *go_hook.HookInput) error {
	fmt.Println("[NON GLOBAL TEST HOOK] get pods")
	pods, err := sdkobjectpatch.UnmarshalToStruct[v1core.Pod](input.NewSnapshots, "test_hook")
	if err != nil {
		return fmt.Errorf("failed to unmarshal pods: %w", err)
	}
	fmt.Println("[NON GLOBAL TEST HOOK] len of pods", len(pods))

	return nil
}
