package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Module kubernetes object
type Module struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Properties ModuleProperties `json:"properties"`

	Status ModuleStatus `json:"status,omitempty"`
}

type ModuleProperties struct {
	Weight int `json:"weight"`
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

func NewModule(moduleName string, order int) *Module {
	return &Module{
		TypeMeta: metav1.TypeMeta{
			APIVersion: moduleGVK.GroupVersion().String(),
			Kind:       moduleGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: moduleName,
		},
		Properties: ModuleProperties{
			Weight: order,
		},
		Status: ModuleStatus{},
	}
}
