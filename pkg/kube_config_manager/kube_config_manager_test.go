package kube_config_manager

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/kubernetes"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/flant/addon-operator/pkg/kube"
	"github.com/flant/addon-operator/pkg/utils"
)

var (
	mockConfigMapList *v1.ConfigMapList
)

type MockConfigMap struct {
	ObjectMeta struct {
		Name string
	}
	Data map[string]string
}

type MockKubernetesClientset struct {
	kubernetes.Interface
}

func (client *MockKubernetesClientset) CoreV1() corev1.CoreV1Interface {
	return MockCoreV1{}
}

type MockCoreV1 struct {
	corev1.CoreV1Interface
}

func (mockCoreV1 MockCoreV1) ConfigMaps(namespace string) corev1.ConfigMapInterface {
	return MockConfigMaps{}
}

type MockConfigMaps struct {
	corev1.ConfigMapInterface
}

func (mockConfigMaps MockConfigMaps) List(options metav1.ListOptions) (*v1.ConfigMapList, error) {
	return mockConfigMapList, nil
}

func (mockConfigMaps MockConfigMaps) Get(name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	for _, v := range mockConfigMapList.Items {
		if v.Name == name {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("no such resource '%s'", name)
}

func (mockConfigMaps MockConfigMaps) Create(obj *v1.ConfigMap) (*v1.ConfigMap, error) {
	mockConfigMapList.Items = append(mockConfigMapList.Items, *obj)
	return obj, nil
}

func (mockConfigMaps MockConfigMaps) Update(obj *v1.ConfigMap) (*v1.ConfigMap, error) {
	for ind, v := range mockConfigMapList.Items {
		if v.Name == obj.Name {
			mockConfigMapList.Items[ind] = *obj
			return obj, nil
		}
	}

	return nil, fmt.Errorf("no such resource '%s'", obj.Name)
}

func Test_Init(t *testing.T) {
	ConfigMapName = "addon-operator"

	cmDataText := `
global: |
  project: tfprod
  clusterName: main
  clusterHostname: kube.flant.com
  settings:
    count: 2
    mysql:
      user: myuser
nginxIngress: | 
  config:
    hsts: true
    setRealIPFrom:
    - 1.1.1.1
    - 2.2.2.2
nginxIngressEnabled: "true"
prometheus: |
  adminPassword: qwerty
  retentionDays: 20
  userPassword: qwerty
kubeLegoEnabled: "false"
`
	cmData := map[string]string{}
	err := yaml.Unmarshal([]byte(cmDataText), cmData)
	assert.NoError(t, err)

	mockConfigMapList = &v1.ConfigMapList{
		Items: []v1.ConfigMap{
			v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName},
				Data: cmData,
			},
		},
	}

	kube.Kubernetes = &MockKubernetesClientset{}

	kcm, err := Init()
	if err != nil {
		t.Errorf("kube_config_manager initialization error: %s", err)
	}
	config := kcm.InitialConfig()

	expectations := map[string] struct {
		isEnabled *bool
		values utils.Values
	}{
		"global": {
			nil,
			utils.Values{
				utils.GlobalValuesKey: map[string]interface{}{
					"project":         "tfprod",
					"clusterName":     "main",
					"clusterHostname": "kube.flant.com",
					"settings": map[string]interface{}{
						"count": 2.0,
						"mysql": map[string]interface{}{
							"user": "myuser",
						},
					},
				},
			},
		},
		"nginx-ingress": {
			&utils.ModuleEnabled,
			utils.Values{
				utils.ModuleNameToValuesKey("nginx-ingress"): map[string]interface{}{
					"config": map[string]interface{}{
						"hsts": true,
						"setRealIPFrom": []interface{}{
							"1.1.1.1",
							"2.2.2.2",
						},
					},
				},
			},
		},
		"prometheus": {
			nil,
			utils.Values{
				utils.ModuleNameToValuesKey("prometheus"): map[string]interface{}{
					"adminPassword": "qwerty",
					"retentionDays": 20.0,
					"userPassword":  "qwerty",
				},
			},
		},
		"kube-lego": {
			&utils.ModuleDisabled,
			utils.Values{},
		},
	}

	for name, expect := range expectations {
		t.Run(name, func(t *testing.T){
			if name == "global" {
				assert.Equal(t, expect.values, config.Values)
			} else {
				// module
				moduleConfig, hasConfig := config.ModuleConfigs[name]
				assert.True(t, hasConfig)
				assert.Equal(t, expect.isEnabled, moduleConfig.IsEnabled)
				assert.Equal(t, expect.values, moduleConfig.Values)
			}
		})
	}
}

func findCurrentConfigMap() *v1.ConfigMap {
	for _, cm := range mockConfigMapList.Items {
		if cm.Name == ConfigMapName {
			return &cm
		}
	}

	return nil
}

func configRawDataShouldEqual(expectedData map[string]string) error {
	obj := findCurrentConfigMap()
	if obj == nil {
		return fmt.Errorf("expected ConfigMap 'addon-operator' to be existing")
	}

	if !reflect.DeepEqual(obj.Data, expectedData) {
		return fmt.Errorf("expected %+v ConfigMap data, got %+v", expectedData, obj.Data)
	}

	return nil
}

func convertToConfigData(values utils.Values) (map[string]string, error) {
	res := make(map[string]string)
	for k, v := range values {
		yamlData, err := yaml.Marshal(v)
		if err != nil {
			return nil, err
		}
		res[k] = string(yamlData)
	}

	return res, nil
}

func configDataShouldEqual(expectedValues utils.Values) error {
	expectedDataRaw, err := convertToConfigData(expectedValues)
	if err != nil {
		return err
	}
	return configRawDataShouldEqual(expectedDataRaw)
}

//
func Test_SetConfig(t *testing.T) {
	mockConfigMapList = &v1.ConfigMapList{}
	kube.Kubernetes = &MockKubernetesClientset{}
	kcm := &MainKubeConfigManager{}

	var err error

	err = kcm.SetKubeGlobalValues(utils.Values{
		utils.GlobalValuesKey: map[string]interface{}{
			"mysql": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = configDataShouldEqual(utils.Values{
		utils.GlobalValuesKey: map[string]interface{}{
			"mysql": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
		},
	})
	if err != nil {
		t.Error(err)
	}

	err = kcm.SetKubeGlobalValues(utils.Values{
		utils.GlobalValuesKey: map[string]interface{}{
			"mysql": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
			"mongo": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = configDataShouldEqual(utils.Values{
		utils.GlobalValuesKey: map[string]interface{}{
			"mysql": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
			"mongo": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
		},
	})
	if err != nil {
		t.Error(err)
	}

	err = kcm.SetKubeModuleValues("mymodule", utils.Values{
		utils.ModuleNameToValuesKey("mymodule"): map[string]interface{}{
			"one": 1,
			"two": 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = configDataShouldEqual(utils.Values{
		utils.GlobalValuesKey: map[string]interface{}{
			"mysql": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
			"mongo": map[string]interface{}{
				"username": "root",
				"password": "password",
			},
		},
		utils.ModuleNameToValuesKey("mymodule"): map[string]interface{}{
			"one": 1,
			"two": 2,
		},
	})
	if err != nil {
		t.Error(err)
	}

}
