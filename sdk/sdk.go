package sdk

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

var _ go_hook.GoHook = (*commonGoHook)(nil)

type reconcileFunc func(input *go_hook.HookInput) error

type commonGoHook struct {
	config        *go_hook.HookConfig
	reconcileFunc reconcileFunc
}

func newCommonGoHook(config *go_hook.HookConfig, reconcileFunc func(input *go_hook.HookInput) error) *commonGoHook {
	return &commonGoHook{config: config, reconcileFunc: reconcileFunc}
}

func (h *commonGoHook) Config() *go_hook.HookConfig {
	return h.config
}

func (h *commonGoHook) Run(input *go_hook.HookInput) error {
	return h.reconcileFunc(input)
}

func ToUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	return &unstructured.Unstructured{Object: content}, err
}

func FromUnstructured(unstructuredObj *unstructured.Unstructured, obj interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), obj)
}
