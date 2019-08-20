// +build !release

package kube

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// Mock object for kubernetes client. This mock supports:
// - get, list, create, update actions for ConfigMaps
// - namespaces are not supported
// - ListOprtions and GetOptions are ignored, search only by name
//
// Usage:
//
//     kubeMock := NewMockKubernetesClientset()
//     kubeMock.ConfigMapList = &v1.ConfigMapList{
//       Items: []v1.ConfigMap{
//         v1.ConfigMap{
//           ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName},
//           Data: cmData,
//         },
//      },
//    }
//    kube.Kubernetes = kubeMock
//


// MockKubernetesClientset has ConfigMapList field where all ConfigMaps are stored.
//
// Each underlying layer of API structs have a pointer to MockKubernetesClientset
// to access to ConfigMapList items.
type MockKubernetesClientset struct {
	kubernetes.Interface
	ConfigMapList *v1.ConfigMapList
}

func NewMockKubernetesClientset() *MockKubernetesClientset {
	return &MockKubernetesClientset{
		ConfigMapList: &v1.ConfigMapList{
			Items: make([]v1.ConfigMap, 0),
		},
	}
}

// Core layer of API
func (client *MockKubernetesClientset) CoreV1() corev1.CoreV1Interface {
	return MockCoreV1{
		mockClient: client,
	}
}

type MockCoreV1 struct {
	corev1.CoreV1Interface
	mockClient *MockKubernetesClientset
}

func (mockCoreV1 MockCoreV1) ConfigMaps(namespace string) corev1.ConfigMapInterface {
	return MockConfigMaps{
		mockClient: mockCoreV1.mockClient,
	}
}


// ConfigMaps layer
type MockConfigMaps struct {
	corev1.ConfigMapInterface
	mockClient *MockKubernetesClientset
}

func (configMaps MockConfigMaps) List(options metav1.ListOptions) (*v1.ConfigMapList, error) {
	return configMaps.mockClient.ConfigMapList, nil
}

func (configMaps MockConfigMaps) Get(name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	for _, v := range configMaps.mockClient.ConfigMapList.Items {
		if v.Name == name {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("no such resource '%s'", name)
}

func (configMaps MockConfigMaps) Create(obj *v1.ConfigMap) (*v1.ConfigMap, error) {
	configMaps.mockClient.ConfigMapList.Items = append(configMaps.mockClient.ConfigMapList.Items, *obj)
	return obj, nil
}

func (configMaps MockConfigMaps) Update(obj *v1.ConfigMap) (*v1.ConfigMap, error) {
	for ind, v := range configMaps.mockClient.ConfigMapList.Items {
		if v.Name == obj.Name {
			configMaps.mockClient.ConfigMapList.Items[ind] = *obj
			return obj, nil
		}
	}

	return nil, fmt.Errorf("no such resource '%s'", obj.Name)
}

