package status_collector

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/shell-operator/pkg/kube/object_patch"
)

// Status collector gathers status patch operations that are being applied to a resource regardless whether the hook reconcile was successful or not
type StatusCollector struct {
	patchCollector *object_patch.PatchCollector
}

func NewCollector() *StatusCollector {
	return &StatusCollector{
		patchCollector: object_patch.NewPatchCollector(),
	}
}

func (sc *StatusCollector) Operations() []object_patch.Operation {
	return sc.patchCollector.Operations()
}

func (sc *StatusCollector) UpdateStatus(filterFunc func(*unstructured.Unstructured) (*unstructured.Unstructured, error), apiVersion, kind, namespace, name string) {
	sc.patchCollector.Filter(filterFunc, apiVersion, kind, namespace, name, object_patch.WithSubresource("/status"))
}
