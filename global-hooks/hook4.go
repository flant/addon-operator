// Copyright 2021 Flant JSC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package global_hooks

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
			Name:       "test_hook4",
			ApiVersion: "v1",
			Kind:       "Service",
			FilterFunc: applyTestHook4Filter,
		},
	},
}, runTestHook4)

func applyTestHook4Filter(obj *unstructured.Unstructured) (go_hook.FilterResult, error) {
	pod := v1core.Service{}
	err := sdk.FromUnstructured(obj, &pod)
	if err != nil {
		return nil, err
	}
	return pod, nil
}

func runTestHook4(input *go_hook.HookInput) error {
	fmt.Println("[TEST HOOK 4] get services")
	pod, err := sdkobjectpatch.UnmarshalToStruct[v1core.Service](input.NewSnapshots, "test_hook4")
	if err != nil {
		return fmt.Errorf("failed to unmarshal pod: %w", err)
	}
	fmt.Println("[TEST HOOK 4] len of services", len(pod))
	return nil
}
