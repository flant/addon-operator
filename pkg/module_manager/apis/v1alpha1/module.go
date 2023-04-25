package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Module struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ModuleSpec `json:"spec"`

	Status ModuleStatus `json:"status,omitempty"`
}

type ModuleSpec struct {
	Name string `json:"name"`
	Foo  string `json:"foo"`
}

type ModuleStatus struct{}

type moduleKind struct{}

func (ms *ModuleStatus) GetObjectKind() schema.ObjectKind {
	return &moduleKind{}
}

func (mk *moduleKind) SetGroupVersionKind(_ schema.GroupVersionKind) {}
func (mk *moduleKind) GroupVersionKind() schema.GroupVersionKind {
	return moduleGVK
}

var moduleGVK = schema.GroupVersionKind{Group: "deckhouse.io", Version: "v1alpha1", Kind: "Module"}

func NewModule(moduleName string) *Module {
	return &Module{
		TypeMeta: metav1.TypeMeta{
			APIVersion: moduleGVK.GroupVersion().String(),
			Kind:       moduleGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: moduleName,
		},
		Spec: ModuleSpec{
			Name: moduleName,
			Foo:  "akak",
		},
		Status: ModuleStatus{},
	}
}
