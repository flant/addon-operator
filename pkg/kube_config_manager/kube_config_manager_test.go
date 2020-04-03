package kube_config_manager

import (
	"context"
	"sync"
	"testing"

	"github.com/flant/addon-operator/pkg/app"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flant/shell-operator/pkg/kube"

	"github.com/flant/addon-operator/pkg/utils"
)

func Test_LoadValues_On_Init(t *testing.T) {
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
	_ = yaml.Unmarshal([]byte(cmDataText), cmData)

	kubeClient := kube.NewFakeKubernetesClient()
	_, _ = kubeClient.CoreV1().ConfigMaps("default").Create(&v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "addon-operator"},
		Data:       cmData,
	})

	kcm := NewKubeConfigManager()
	kcm.WithKubeClient(kubeClient)
	kcm.WithNamespace("default")
	kcm.WithConfigMapName("addon-operator")

	err := kcm.Init()
	if err != nil {
		t.Errorf("kube_config_manager initialization error: %s", err)
	}
	config := kcm.InitialConfig()

	tests := map[string]struct {
		isEnabled *bool
		values    utils.Values
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

	for name, expect := range tests {
		t.Run(name, func(t *testing.T) {
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

func Test_SaveValuesToConfigMap(t *testing.T) {
	kubeClient := kube.NewFakeKubernetesClient()

	kcm := &kubeConfigManager{}
	kcm.WithKubeClient(kubeClient)
	kcm.WithNamespace("default")
	kcm.WithConfigMapName("addon-operator")

	var err error
	var cm *v1.ConfigMap

	tests := []struct {
		name         string
		globalValues *utils.Values
		moduleValues *utils.Values
		moduleName   string
		testFn       func(global *utils.Values, module *utils.Values)
	}{
		{
			"scenario 1: first save with non existent ConfigMap",
			&utils.Values{
				utils.GlobalValuesKey: map[string]interface{}{
					"mysql": map[string]interface{}{
						"username": "root",
						"password": "password",
					},
				},
			},
			nil,
			"",
			func(global *utils.Values, module *utils.Values) {
				// Check values in a 'global' key
				assert.Contains(t, cm.Data, "global", "ConfigMap should contain a 'global' key")
				savedGlobalValues, err := utils.NewGlobalValues(cm.Data["global"])
				if assert.NoError(t, err, "ConfigMap should be created") {
					assert.Equal(t, *global, savedGlobalValues)
				}
			},
		},
		{
			"scenario 2: add more values to global key",
			&utils.Values{
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
			},
			nil, "",
			func(global *utils.Values, module *utils.Values) {
				// Check values in a 'global' key
				assert.Contains(t, cm.Data, "global", "ConfigMap should contain a 'global' key")
				savedGlobalValues, err := utils.NewGlobalValues(cm.Data["global"])
				if assert.NoError(t, err, "ConfigMap should be created") {
					assert.Equal(t, *global, savedGlobalValues)
				}
			},
		},
		{
			"scenario 3: save module values",
			nil,
			&utils.Values{
				utils.ModuleNameToValuesKey("mymodule"): map[string]interface{}{
					"one": 1.0,
					"two": 2.0,
				},
			},
			"mymodule",
			func(global *utils.Values, module *utils.Values) {
				// Check values in a 'global' key
				assert.Contains(t, cm.Data, "global", "ConfigMap should contain a 'global' key")

				savedGlobalValues, err := utils.NewGlobalValues(cm.Data["global"])
				if assert.NoError(t, err, "ConfigMap should be created") {
					assert.Equal(t, utils.Values{
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
					}, savedGlobalValues)
				} else {
					t.FailNow()
				}

				assert.Contains(t, cm.Data, utils.ModuleNameToValuesKey("mymodule"), "ConfigMap should contain a '%s' key", utils.ModuleNameToValuesKey("mymodule"))
				mconf, err := ExtractModuleKubeConfig("mymodule", cm.Data)
				if assert.NoError(t, err, "ModuleConfig should load") {
					assert.Equal(t, *module, mconf.Values)
				} else {
					t.FailNow()
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.globalValues != nil {
				err = kcm.SetKubeGlobalValues(*test.globalValues)
				if !assert.NoError(t, err, "Global Values should be saved") {
					t.FailNow()
				}
			} else if test.moduleValues != nil {
				err = kcm.SetKubeModuleValues(test.moduleName, *test.moduleValues)
				if !assert.NoError(t, err, "Module Values should be saved") {
					t.FailNow()
				}
			}

			// Check that ConfigMap is created or exists
			cm, err = kubeClient.CoreV1().ConfigMaps("default").Get("addon-operator", metav1.GetOptions{})
			if assert.NoError(t, err, "ConfigMap should exist after SetKubeGlobalValues") {
				assert.NotNil(t, cm, "ConfigMap should not be nil")
			} else {
				t.FailNow()
			}

			test.testFn(test.globalValues, test.moduleValues)
		})
	}

}

// Receive message over ModuleConfigsUpdate when ConfigMap is
// externally modified.
func TestKubeConfigManager_ModuleConfigsUpdated_chan(t *testing.T) {
	g := NewWithT(t)

	kubeClient := kube.NewFakeKubernetesClient()

	cm := &v1.ConfigMap{}
	cm.SetNamespace("default")
	cm.SetName(app.ConfigMapName)
	cm.SetAnnotations(map[string]string{
		app.ValuesChecksumsAnnotation: `{"module1":"asdasdzxczxcqweqwe"}`,
	})
	cm.Data = map[string]string{
		"global": `
param1: val1
param2: val2
`,
	}

	_, err := kubeClient.CoreV1().ConfigMaps("default").Create(cm)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be created")

	kcm := NewKubeConfigManager()
	kcm.WithContext(context.Background())
	kcm.WithKubeClient(kubeClient)
	kcm.WithNamespace("default")
	kcm.WithConfigMapName(app.ConfigMapName)
	kcm.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

	err = kcm.Init()
	g.Expect(err).ShouldNot(HaveOccurred(), "KubeConfigManager should init correctly")

	go kcm.Start()
	defer kcm.Stop()

	var newModuleConfigs ModuleConfigs

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		newModuleConfigs = <-ModuleConfigsUpdated
		wg.Done()
	}()

	// update cm
	cm.Data["module2"] = `
modParam1: val1
modParam2: val2
`
	_, err = kubeClient.CoreV1().ConfigMaps("default").Update(cm)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be created")

	wg.Wait()

	g.Expect(newModuleConfigs).To(HaveLen(1))
}

// SetKubeModuleValues should update ConfigMap's data
func TestKubeConfigManager_SetKubeModuleValues(t *testing.T) {
	g := NewWithT(t)

	kubeClient := kube.NewFakeKubernetesClient()

	cm := &v1.ConfigMap{}
	cm.SetNamespace("default")
	cm.SetName(app.ConfigMapName)
	cm.SetAnnotations(map[string]string{
		app.ValuesChecksumsAnnotation: `{"module1":"asdasdzxczxcqweqwe"}`,
	})
	cm.Data = map[string]string{
		"global": `
param1: val1
param2: val2
`,
	}

	_, err := kubeClient.CoreV1().ConfigMaps("default").Create(cm)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be created")

	kcm := NewKubeConfigManager()
	kcm.WithContext(context.Background())
	kcm.WithKubeClient(kubeClient)
	kcm.WithNamespace("default")
	kcm.WithConfigMapName(app.ConfigMapName)
	kcm.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

	err = kcm.Init()
	g.Expect(err).ShouldNot(HaveOccurred(), "KubeConfigManager should init correctly")

	go kcm.Start()
	defer kcm.Stop()

	// Set modules values
	modVals, err := utils.NewValuesFromBytes([]byte(`
moduleLongName:
  modLongParam1: val1
  modLongParam2: val2
`))
	g.Expect(err).ShouldNot(HaveOccurred(), "values should load from bytes")
	g.Expect(modVals).To(HaveKey("moduleLongName"))

	err = kcm.SetKubeModuleValues("module-long-name", modVals)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Check that values are updated in ConfigMap
	cm, err = kubeClient.CoreV1().ConfigMaps("default").Get(app.ConfigMapName, metav1.GetOptions{})
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap get")

	g.Expect(cm.Data).Should(HaveLen(2))
	g.Expect(cm.Data).To(HaveKey("global"))
	g.Expect(cm.Data).To(HaveKey("moduleLongName"))

	g.Expect(cm.Annotations).To(HaveKey(app.ValuesChecksumsAnnotation))
	// chacksum annotation should contain an initial key 'module1' and a new key 'module-long-name'
	anno := cm.Annotations[app.ValuesChecksumsAnnotation]

	g.Expect(anno).To(ContainSubstring("module-long-name"))
	g.Expect(anno).To(ContainSubstring("module1"))
}
