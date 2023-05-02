package v1alpha1

import (
	"strings"

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
	Weight int      `json:"weight"`
	Labels []string `json:"labels"`
}

type ModuleStatus struct{}

type moduleKind struct{}

func (ms *ModuleStatus) GetObjectKind() schema.ObjectKind {
	return &moduleKind{}
}

func (mk *moduleKind) SetGroupVersionKind(_ schema.GroupVersionKind) {}
func (mk *moduleKind) GroupVersionKind() schema.GroupVersionKind {
	return ModuleGVK
}

var ModuleGVK = schema.GroupVersionKind{Group: "deckhouse.io", Version: "v1alpha1", Kind: "Module"}

func NewModule(moduleName string, order int) *Module {
	m := &Module{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ModuleGVK.GroupVersion().String(),
			Kind:       ModuleGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: moduleName,
		},
		Properties: ModuleProperties{
			Weight: order,
			Labels: make([]string, 0),
		},
		Status: ModuleStatus{},
	}

	m.calculateLabels()

	return m
}

func (m *Module) calculateLabels() {
	if strings.HasPrefix(m.Name, "cni-") {
		m.Properties.Labels = append(m.Properties.Labels, "cni")
	}

	if strings.HasPrefix(m.Name, "cloud-provider-") {
		m.Properties.Labels = append(m.Properties.Labels, "cloud-provider")
	}

	if strings.HasSuffix(m.Name, "-crd") {
		m.Properties.Labels = append(m.Properties.Labels, "crd")
	}

	// upper part could be removed when we will ready properties from the module.yaml file

	for _, label := range m.Properties.Labels {
		m.Labels["module.deckhouse.io/"+label] = ""
	}
}
