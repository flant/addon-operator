package kube_config_manager

import (
	"context"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/flant/addon-operator/pkg/kube_config_manager/backend/configmap"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
	klient "github.com/flant/kube-client/client"
)

const testConfigMapName = "test-addon-operator"

// initKubeConfigManager returns an initialized KubeConfigManager instance.
// Pass string or map to prefill ConfigMap.
func initKubeConfigManager(t *testing.T, kubeClient *klient.Client, cmData map[string]string, cmContent string) *KubeConfigManager {
	g := NewWithT(t)

	cm := &v1.ConfigMap{}
	cm.SetNamespace("default")
	cm.SetName(testConfigMapName)

	if cmData != nil {
		cm.Data = cmData
	} else {
		cmData := map[string]string{}
		_ = yaml.Unmarshal([]byte(cmContent), cmData)
		cm.Data = cmData
	}

	_, err := kubeClient.CoreV1().ConfigMaps("default").Create(context.TODO(), cm, metav1.CreateOptions{})
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be created")

	bk := configmap.New(kubeClient, "default", testConfigMapName, log.NewNop())
	kcm := NewKubeConfigManager(context.Background(), bk, nil, log.NewNop())

	err = kcm.Init()
	g.Expect(err).ShouldNot(HaveOccurred(), "KubeConfigManager should init correctly")

	kcm.Start()

	return kcm
}

func Test_KubeConfigManager_loadConfig(t *testing.T) {
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
grafanaEnabled: "false"
kubeDns: |
  config:
    resolver: "192.168.0.1"
kubeDnsEnabled: "true"
kubeDnsManagementState: "Unmanaged"
`

	kubeClient := klient.NewFake(nil)

	kcm := initKubeConfigManager(t, kubeClient, nil, cmDataText)

	defer kcm.Stop()

	tests := map[string]struct {
		isEnabled       *bool
		managementState utils.ManagementState
		values          utils.Values
	}{
		"global": {
			nil,
			utils.Managed,
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
			utils.Managed,
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
			utils.Managed,
			utils.Values{
				utils.ModuleNameToValuesKey("prometheus"): map[string]interface{}{
					"adminPassword": "qwerty",
					"retentionDays": 20.0,
					"userPassword":  "qwerty",
				},
			},
		},
		"grafana": {
			&utils.ModuleDisabled,
			utils.Managed,
			utils.Values{},
		},
		"kube-dns": {
			&utils.ModuleEnabled,
			utils.Unmanaged,
			utils.Values{
				utils.ModuleNameToValuesKey("kubeDns"): map[string]interface{}{
					"config": map[string]interface{}{
						"resolver": "192.168.0.1",
					},
				},
			},
		},
	}

	// No need to use lock in KubeConfigManager.
	for name, expect := range tests {
		t.Run(name, func(t *testing.T) {
			if name == "global" {
				kcm.SafeReadConfig(func(config *config.KubeConfig) {
					assert.Equal(t, expect.values, config.Global.Values)
				})
			} else {
				kcm.SafeReadConfig(func(config *config.KubeConfig) {
					// module
					moduleConfig, hasConfig := config.Modules[name]
					assert.True(t, hasConfig)
					assert.Equal(t, expect.isEnabled, moduleConfig.IsEnabled)
					assert.Equal(t, expect.managementState, moduleConfig.ManagementState)
					assert.Equal(t, expect.values, moduleConfig.GetValuesWithModuleName()) //nolint: staticcheck,nolintlint
				})
			}
		})
	}
}

func Test_KubeConfigManager_SaveValuesToConfigMap(t *testing.T) {
	kubeClient := klient.NewFake(nil)

	kcm := initKubeConfigManager(t, kubeClient, nil, "")

	var err error
	var cm *v1.ConfigMap

	tests := []struct {
		name         string
		globalValues *utils.Values
		moduleValues *utils.Values
		moduleName   string
		testFn       func(t *testing.T, global *utils.Values, module *utils.Values)
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
			func(t *testing.T, global *utils.Values, _ *utils.Values) {
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
			func(t *testing.T, global *utils.Values, _ *utils.Values) {
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
			func(t *testing.T, _ *utils.Values, _ *utils.Values) {
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
				// mconf, err := ExtractModuleKubeConfig("mymodule", cm.Data)
				// if assert.NoError(t, err, "ModuleConfig should load") {
				//	assert.Equal(t, *module, mconf.Values)
				// } else {
				//	t.FailNow()
				// }
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.globalValues != nil {
				err = kcm.SaveConfigValues(utils.GlobalValuesKey, *test.globalValues)
				if !assert.NoError(t, err, "Global Values should be saved") {
					t.FailNow()
				}
			} else if test.moduleValues != nil {
				err = kcm.SaveConfigValues(test.moduleName, *test.moduleValues)
				if !assert.NoError(t, err, "Module Values should be saved") {
					t.FailNow()
				}
			}

			// Check that ConfigMap is created or exists
			cm, err = kubeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), testConfigMapName, metav1.GetOptions{})
			if assert.NoError(t, err, "ConfigMap should exist after SaveGlobalConfigValues") {
				assert.NotNil(t, cm, "ConfigMap should not be nil")
			} else {
				t.FailNow()
			}

			test.testFn(t, test.globalValues, test.moduleValues)
		})
	}
}

// Receive message over KubeConfigEventCh when ConfigMap is
// externally modified.
func Test_KubeConfigManager_event_after_adding_module_section(t *testing.T) {
	g := NewWithT(t)

	kubeClient := klient.NewFake(nil)

	kcm := initKubeConfigManager(t, kubeClient, map[string]string{
		"global": `
param1: val1
param2: val2
`,
	}, "")

	defer kcm.Stop()

	// Check initial modules configs.
	kcm.SafeReadConfig(func(config *config.KubeConfig) {
		g.Expect(config.Modules).To(HaveLen(0), "No modules section should be after Init()")
	})

	// Update ConfigMap with new module section.
	sectionPatch := `[{"op": "add", 
"path": "/data/module2",
"value": "modParam1: val1\nmodParam2: val2"}]`

	_, err := kubeClient.CoreV1().ConfigMaps("default").Patch(context.TODO(),
		testConfigMapName,
		types.JSONPatchType,
		[]byte(sectionPatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")

	// Wait for event.
	g.Eventually(kcm.KubeConfigEventCh(), "20s", "100ms").Should(Receive(), "KubeConfigManager should emit event")

	kcm.SafeReadConfig(func(config *config.KubeConfig) {
		g.Expect(config.Modules).To(HaveLen(1), "Module section should appear after ConfigMap update")
		g.Expect(config.Modules).To(HaveKey("module2"), "module2 section should appear after ConfigMap update")
	})

	// Update ConfigMap with global section.
	globalPatch := `[{"op": "replace", 
"path": "/data/global",
"value": "param1: val1"}]`

	_, err = kubeClient.CoreV1().ConfigMaps("default").Patch(context.TODO(),
		testConfigMapName,
		types.JSONPatchType,
		[]byte(globalPatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")

	// Wait for event.
	g.Eventually(kcm.KubeConfigEventCh(), "20s", "100ms").Should(Receive(), "KubeConfigManager should emit event")

	kcm.SafeReadConfig(func(config *config.KubeConfig) {
		g.Expect(config.Global.Values).To(HaveKey("global"), "Should update global section cache")
		g.Expect(config.Global.Values["global"]).To(HaveKey("param1"), "Should update global section cache")
	})
}

// SaveModuleConfigValues should update ConfigMap's data
func Test_KubeConfigManager_SaveModuleConfigValues(t *testing.T) {
	g := NewWithT(t)
	kubeClient := klient.NewFake(nil)

	kcm := initKubeConfigManager(t, kubeClient, map[string]string{
		"global": `
param1: val1
param2: val2
`,
	}, "")

	defer kcm.Stop()

	// Set modules values
	modVals, err := utils.NewValuesFromBytes([]byte(`
moduleLongName:
  modLongParam1: val1
  modLongParam2: val2
`))
	g.Expect(err).ShouldNot(HaveOccurred(), "values should load from bytes")
	g.Expect(modVals).To(HaveKey("moduleLongName"))

	err = kcm.SaveConfigValues("module-long-name", modVals)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Check that values are updated in ConfigMap
	cm, err := kubeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), testConfigMapName, metav1.GetOptions{})
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap get")

	g.Expect(cm.Data).Should(HaveLen(2))
	g.Expect(cm.Data).To(HaveKey("global"))
	g.Expect(cm.Data).To(HaveKey("moduleLongName"))
}

// Check error if ConfigMap on init
func Test_KubeConfigManager_error_on_Init(t *testing.T) {
	g := NewWithT(t)

	kubeClient := klient.NewFake(nil)
	kcm := initKubeConfigManager(t, kubeClient, nil, "global: ''")
	defer kcm.Stop()

	// Update ConfigMap with new module section with bad name.
	badNameSectionPatch := `[{"op": "add", 
"path": "/data/InvalidName-module",
"value": "modParam1: val1\nmodParam2: val2"}]`

	_, err := kubeClient.CoreV1().ConfigMaps("default").Patch(context.TODO(),
		testConfigMapName,
		types.JSONPatchType,
		[]byte(badNameSectionPatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")

	// Wait for event
	ev := <-kcm.KubeConfigEventCh()

	g.Expect(ev).To(Equal(config.KubeConfigEvent{Type: config.KubeConfigInvalid}), "Invalid name in module section should generate 'invalid' event")

	// kcm.SafeReadConfig(func(config *KubeConfig) {
	//	g.Expect(config.IsInvalid).To(Equal(true), "Current config should be invalid")
	// })

	// Update ConfigMap with new module section with bad name.
	validSectionPatch := `[{"op": "add", 
"path": "/data/validModuleName",
"value": "modParam1: val1\nmodParam2: val2"},
{"op": "add", 
"path": "/data/validModuleNameManagementState",
"value": "Unmanaged"},
{"op": "remove", 
"path": "/data/InvalidName-module"}]`

	_, err = kubeClient.CoreV1().ConfigMaps("default").Patch(context.TODO(),
		testConfigMapName,
		types.JSONPatchType,
		[]byte(validSectionPatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")

	// Wait for event
	ev = <-kcm.KubeConfigEventCh()

	g.Expect(ev).To(Equal(config.KubeConfigEvent{
		Type:                      config.KubeConfigChanged,
		ModuleEnabledStateChanged: []string{},
		ModuleValuesChanged:       []string{"valid-module-name"},
		GlobalSectionChanged:      false,
		ModuleManagementStateChanged: map[string]utils.ManagementState{
			"valid-module-name": "Unmanaged",
		},
	}), "Valid section patch should generate 'changed' event")

	kcm.SafeReadConfig(func(config *config.KubeConfig) {
		// g.Expect(config.IsInvalid).To(Equal(false), "Current config should be valid")
		g.Expect(config.Modules).To(HaveLen(1), "Current config should have module sections")
		g.Expect(config.Modules).To(HaveKey("valid-module-name"), "Current config should have module section for 'valid-module-name'")
		modValues := config.Modules["valid-module-name"].GetValuesWithModuleName() //nolint: staticcheck,nolintlint
		g.Expect(modValues.HasKey("validModuleName")).To(BeTrue())
		m := modValues["validModuleName"]
		vals := m.(map[string]interface{})
		g.Expect(vals).To(HaveKey("modParam2"), "Module config values should contain modParam2 key")
	})
}
