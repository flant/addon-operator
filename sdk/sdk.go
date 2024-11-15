package sdk

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go-hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/sdk/registry"
)

var RegisterFunc = func(config *gohook.HookConfig, reconcileFunc kind.ReconcileFunc) bool {
	registry.Registry().Add(kind.NewGoHook(config, reconcileFunc))
	return true
}

func ToUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	return &unstructured.Unstructured{Object: content}, err
}

func FromUnstructured(unstructuredObj *unstructured.Unstructured, obj interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), obj)
}
