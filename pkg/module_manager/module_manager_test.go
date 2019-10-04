package module_manager

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/romana/rlog"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"

	"github.com/flant/shell-operator/pkg/kube"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/utils"
)


// initModuleManager is a test version of an Init method
func initModuleManager(t *testing.T, mm *MainModuleManager, configPath string) {
	EventCh = make(chan Event, 1)

	rootDir := filepath.Join("testdata", configPath)

	var err error
	tempDir, err := ioutil.TempDir("", "addon-operator-")
	rlog.Infof("TEMP DIR %s", tempDir)
	if err != nil {
		t.Fatal(err)
	}

	mm.WithDirectories(filepath.Join(rootDir, "modules"), filepath.Join(rootDir, "global-hooks"), tempDir)

	if err := mm.initModulesIndex(); err != nil {
		t.Fatal(err)
	}

	if err := mm.RegisterGlobalHooks(); err != nil {
		t.Fatal(err)
	}

	cmFilePath := filepath.Join(rootDir, "config_map.yaml")
	exists, _ := utils_file.FileExists(cmFilePath)
	if exists {
		cmDataBytes, err := ioutil.ReadFile(cmFilePath)
		if err != nil {
			t.Fatalf("congig map file '%s': %s", cmFilePath, err)
		}

		var cmObj = new(v1.ConfigMap)
		_ = yaml.Unmarshal(cmDataBytes, &cmObj)

		kube.Kubernetes = fake.NewSimpleClientset()
		_, _ = kube.Kubernetes.CoreV1().ConfigMaps("default").Create(cmObj)

		KubeConfigManager := kube_config_manager.NewKubeConfigManager()
		KubeConfigManager.WithNamespace("default")
		KubeConfigManager.WithConfigMapName("addon-operator")
		KubeConfigManager.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

		err = KubeConfigManager.Init()
		if err != nil {
			t.Fatalf("KubeConfigManager.Init(): %v", err)
		}
		mm.WithKubeConfigManager(KubeConfigManager)

		kubeConfig := KubeConfigManager.InitialConfig()
		mm.kubeGlobalConfigValues = kubeConfig.Values

		mm.enabledModulesByConfig, mm.kubeModulesConfigValues, _ = mm.calculateEnabledModulesByConfig(kubeConfig.ModuleConfigs)

	} else {
		mm.enabledModulesByConfig, mm.kubeModulesConfigValues, _ = mm.calculateEnabledModulesByConfig(kube_config_manager.ModuleConfigs{})
	}
}

func Test_MainModuleManager_LoadValuesInInit(t *testing.T) {
	var mm *MainModuleManager

	tests := []struct {
		name       string
		configPath string
		testFn     func()
	}{
		{
			"only_global",
			"load_values__common_static_global_only",
			func() {
				expectedValues := utils.Values{
					"global": map[string]interface{}{
						"a": 1.0,
						"b": 2.0,
						"c": 3.0,
						"d": []interface{}{"a", "b", "c"},
					},
				}

				assert.Equal(t, expectedValues, mm.commonStaticValues, "all common values")
				assert.Equal(t, expectedValues, mm.globalCommonStaticValues, "global section of common values")
			},
		},
		{
			"only_module_config",
			"load_values__module_static_only",
			func() {
				modWithValues1Expected :=  utils.Values{
				"withValues1": map[string]interface{}{
					"a": 1.0, "b": 2.0, "c": 3.0,
				},
				}

				modWithValues2Expected := utils.Values{
					"withValues2": map[string]interface{}{
						"a": []interface{}{1.0, 2.0, map[string]interface{}{"b": 3.0}},
					},
				}

				assert.Contains(t, mm.allModulesByName, "with-values-1")
				assert.Contains(t, mm.allModulesByName, "with-values-2")

				with1 := mm.allModulesByName["with-values-1"]
				assert.NotNil(t, with1.StaticConfig)
				assert.Equal(t, modWithValues1Expected, with1.StaticConfig.Values)

				with2 := mm.allModulesByName["with-values-2"]
				assert.NotNil(t, with2.StaticConfig)
				assert.Equal(t, modWithValues2Expected, with2.StaticConfig.Values)
			},
		},
		{
			// no values.yaml files, but modules are loaded properly
			"empty",
			"load_values__common_static_empty",
			func() {
				assert.Len(t, mm.commonStaticValues, 0)
				assert.Len(t, mm.globalCommonStaticValues, 0)
				assert.Len(t, mm.allModulesByName, 1)
				assert.NotNil(t, mm.allModulesByName["module"].CommonStaticConfig)
				assert.NotNil(t, mm.allModulesByName["module"].StaticConfig)
			},
		},
		{
			"mixed",
			"load_values__common_and_module_and_kube",
			func() {
				assert.Len(t, mm.commonStaticValues, 4)
				assert.Len(t, mm.globalCommonStaticValues, 1)
				assert.Len(t, mm.allModulesByName, 4)

				assert.Contains(t, mm.allModulesByName, "with-values-1")
				assert.Contains(t, mm.allModulesByName, "with-values-2")
				assert.Contains(t, mm.allModulesByName, "without-values")
				assert.Contains(t, mm.allModulesByName, "with-kube-values")

				with1 := mm.allModulesByName["with-values-1"]
				assert.NotNil(t, with1.CommonStaticConfig)
				assert.NotNil(t, with1.StaticConfig)
				assert.Equal(t, "with-values-1", with1.CommonStaticConfig.ModuleName)
				assert.Equal(t, "withValues1", with1.CommonStaticConfig.ModuleConfigKey)
				assert.Equal(t, "withValues1Enabled", with1.CommonStaticConfig.ModuleEnabledKey)
				assert.Equal(t, "with-values-1", with1.StaticConfig.ModuleName)

				// with-values-1 is enabled by common values.yaml
				assert.True(t, *with1.CommonStaticConfig.IsEnabled)
				assert.False(t, *with1.StaticConfig.IsEnabled)

				assert.Len(t, with1.CommonStaticConfig.Values["withValues1"], 1)
				assert.Len(t, with1.StaticConfig.Values["withValues1"], 3)
				// with-values-1 has "a" value in common and in module static values
				assert.Contains(t, with1.CommonStaticConfig.Values["withValues1"], "a")
				assert.Contains(t, with1.StaticConfig.Values["withValues1"], "a")

				assert.NotContains(t, mm.kubeModulesConfigValues, "with-values-1")

				// all modules should have CommonStaticConfig and StaticConfig
				assert.NotNil(t, mm.allModulesByName["with-values-2"].CommonStaticConfig)
				assert.NotNil(t, mm.allModulesByName["with-values-2"].StaticConfig)
				assert.NotNil(t, mm.allModulesByName["without-values"].CommonStaticConfig)
				assert.NotNil(t, mm.allModulesByName["without-values"].StaticConfig)
				assert.NotNil(t, mm.allModulesByName["with-kube-values"].CommonStaticConfig)
				assert.NotNil(t, mm.allModulesByName["with-kube-values"].StaticConfig)

				fmt.Printf("kubeModulesConfigValues: %#v\n", mm.kubeModulesConfigValues)

				// with-values-2 has kube config but disabled
				assert.NotContains(t, mm.kubeModulesConfigValues, "with-values-2")
				// with-kube-values has kube config and is enabled
				assert.Contains(t, mm.kubeModulesConfigValues, "with-kube-values")

				// with-values-1 is enabled in common static config but disabled in module static config
				assert.NotContains(t, mm.enabledModulesByConfig, "with-values-1")
				// with-values-2 is enabled by module static config but disabled in ConfigMap
				assert.NotContains(t, mm.enabledModulesByConfig, "with-values-2")
				// without-values has no values and is disabled by default
				assert.NotContains(t, mm.enabledModulesByConfig, "without-values")
				// with-kube-values is enabled in ConfigMap
				assert.Contains(t, mm.enabledModulesByConfig, "with-kube-values")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mm = NewMainModuleManager()
			initModuleManager(t, mm, test.configPath)
			test.testFn()
		})
	}

}


func Test_MainModuleManager_Get_Module(t *testing.T) {
	mm := NewMainModuleManager()

	initModuleManager(t, mm, "get__module")

	programmaticModule := &Module{Name: "programmatic-module"}
	mm.allModulesByName["programmatic-module"] = programmaticModule

	var module *Module
	var err error


	tests := []struct {
		name       string
		moduleName string
		testFn     func()
	} {
		{
			"module_loaded_from_files",
			"module",
			func() {
				expectedModule := &Module{
					Name:          "module",
					Path:          filepath.Join(mm.ModulesDir, "000-module"),
					DirectoryName: "000-module",
					CommonStaticConfig: &utils.ModuleConfig{
						ModuleName:       "module",
						Values:           utils.Values{},
						IsEnabled:        nil,
						IsUpdated:        false,
						ModuleConfigKey:  "module",
						ModuleEnabledKey: "moduleEnabled",
						RawConfig:        []string{},
					},
					StaticConfig: &utils.ModuleConfig{
						ModuleName:       "module",
						Values:           utils.Values{},
						IsEnabled:        nil,
						IsUpdated:        false,
						ModuleConfigKey:  "module",
						ModuleEnabledKey: "moduleEnabled",
						RawConfig:        []string{},
					},
					moduleManager: mm,
				}
				if assert.NoError(t, err) {
					assert.Equal(t, expectedModule, module)
				}
			},
		},
		{
			"direct_add_module_to_index",
			"programmatic-module",
			func(){
				if assert.NoError(t, err) {
					assert.Equal(t, programmaticModule, module)
				}
			},
		},
		{
			"error-on-non-existent-module",
			"non-existent",
			func() {
				assert.Error(t, err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module = nil
			err = nil
			module, err = mm.GetModule(test.moduleName)
			test.testFn()
		})
	}
}

//func Test_MainModuleManager_Get_ModuleHook(t *testing.T) {
//	t.SkipNow()
//	mm := NewMainModuleManager()
//
//	initModuleManager(t, mm, "get__module_hook")
//
//	var moduleHook *ModuleHook
//	var err error
//
//	tests := []struct{
//		name string
//		hookName string
//		testFn func()
//	} {
//		{
//			"module-hook-all-bindings",
//			"000-all-bindings/hooks/all",
//			func() {
//				expectedHook := &ModuleHook{
//					&CommonHook{
//						"000-all-bindings/hooks/all",
//						filepath.Join(mm.ModulesDir, "000-all-bindings/hooks/all"),
//						[]BindingType{BeforeHelm, AfterHelm, AfterDeleteHelm, OnStartup, Schedule, KubeEvents},
//						map[BindingType]float64 {
//							BeforeHelm:      1.0,
//							AfterHelm:       1.0,
//							AfterDeleteHelm: 1.0,
//							OnStartup:       1.0,
//						},
//						mm,
//					},
//					&Module{},
//					&ModuleHookConfig{
//						HookConfig{
//							1.0,
//							[]schedule_manager.ScheduleConfig{
//								{
//									Crontab:      "* * * * *",
//									AllowFailure: true,
//								},
//							},
//							[]kube_events_manager.OnKubernetesEventConfig{
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{kube_events_manager.KubernetesEventOnAdd},
//									Kind:       "configmap",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component1",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: []string{"namespace1"},
//										Any:        false,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field1)",
//									AllowFailure: true,
//								},
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{
//										kube_events_manager.KubernetesEventOnAdd,
//										kube_events_manager.KubernetesEventOnUpdate,
//										kube_events_manager.KubernetesEventOnDelete,
//									},
//									Kind: "namespace",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component2",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: []string{"namespace2"},
//										Any:        false,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field2)",
//									AllowFailure: true,
//								},
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{
//										kube_events_manager.KubernetesEventOnAdd,
//										kube_events_manager.KubernetesEventOnUpdate,
//										kube_events_manager.KubernetesEventOnDelete,
//									},
//									Kind: "pod",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component3",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: nil,
//										Any:        true,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field3)",
//									AllowFailure: true,
//								},
//							},
//						},
//						1.0,
//						1.0,
//						1.0,
//					},
//				}
//				if assert.NoError(t, err) {
//					moduleHook.Module = &Module{}
//					assert.Equal(t, expectedHook, moduleHook)
//				}
//			},
//		},
//		{
//			"nested-module-hook",
//			"100-nested-hooks/hooks/sub/sub/nested-before-helm",
//			func() {
//				expectedHook := &ModuleHook{
//					&CommonHook{
//						"100-nested-hooks/hooks/sub/sub/nested-before-helm",
//						filepath.Join(mm.ModulesDir, "100-nested-hooks/hooks/sub/sub/nested-before-helm"),
//						[]BindingType{BeforeHelm},
//						map[BindingType]float64 {
//							BeforeHelm: 1.0,
//						},
//						mm,
//					},
//					&Module{},
//					&ModuleHookConfig{
//						HookConfig{
//							OnStartup: nil,
//							Schedule: nil,
//							OnKubernetesEvent: nil,
//						},
//						1.0,
//						nil,
//						nil,
//					},
//				}
//				if assert.NoError(t, err) {
//					moduleHook.Module = &Module{}
//					assert.Equal(t, expectedHook, moduleHook)
//				}
//			},
//		},
//		{
//			"error-on-non-existent-module-hook",
//			"non-existent",
//			func() {
//				assert.Error(t, err)
//			},
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			moduleHook = nil
//			err = nil
//			moduleHook, err = mm.GetModuleHook(test.hookName)
//			test.testFn()
//		})
//	}
//}

func Test_MainModuleManager_Get_ModuleHooksInOrder(t *testing.T) {
	helm.Client = &helm.MockHelmClient{}

	mm := NewMainModuleManager()

	initModuleManager(t, mm, "get__module_hooks_in_order")

	_, _ = mm.DiscoverModulesState()

	var moduleHooks []string
	var err error

	tests := []struct{
		name string
		moduleName string
		bindingType BindingType
		testFn func()
	} {
		{
			"sorted-hooks",
			"after-helm-binding-hooks",
			AfterHelm,
			func() {
				assert.Len(t, mm.allModulesByName, 1)
				assert.Len(t, mm.modulesHooksOrderByName, 1)

				expectedOrder := []string{
					"107-after-helm-binding-hooks/hooks/b",
					"107-after-helm-binding-hooks/hooks/c",
					"107-after-helm-binding-hooks/hooks/a",
				}


				if assert.NoError(t, err) {
					assert.Equal(t, expectedOrder, moduleHooks)
				}
			},
		},
		{
			"no-hooks-for-binding",
			"after-helm-binding-hooks",
			BeforeHelm,
			func() {
				if assert.NoError(t, err) {
					assert.Equal(t, []string{}, moduleHooks)
				}
			},
		},
		{
			"error-on-non-existent-module",
			"after-helm-binding-hookssss",
			BeforeHelm,
			func() {
				assert.Error(t, err)
				assert.Nil(t, moduleHooks)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			moduleHooks = nil
			err = nil
			moduleHooks, err = mm.GetModuleHooksInOrder(test.moduleName, test.bindingType)
			test.testFn()
		})
	}
}


type MockKubeConfigManager struct {
	kube_config_manager.KubeConfigManager
}

func (kcm MockKubeConfigManager) SetKubeGlobalValues(values utils.Values) error {
	return nil
}

func (kcm MockKubeConfigManager) SetKubeModuleValues(moduleName string, values utils.Values) error {
	return nil
}

func Test_MainModuleManager_RunModule(t *testing.T) {
	// TODO something wrong here with patches from afterHelm and beforeHelm hooks
	t.SkipNow()
	hc := &helm.MockHelmClient{}
	helm.Client = hc

	mm := NewMainModuleManager()

	mm.WithKubeConfigManager(MockKubeConfigManager{})

	initModuleManager(t, mm, "test_run_module")

	moduleName := "module"
	expectedModuleValues := utils.Values{
		"global": map[string]interface{}{
			"enabledModules": []string{},
		},
		"module": map[string]interface{}{
			"afterHelm":    "override-value",
			"beforeHelm":   "override-value",
			"replicaCount": 1.0,
			"image": map[string]interface{}{
				"repository": "nginx",
				"tag":        "stable",
				"pullPolicy": "IfNotPresent",
			},
		},
	}

	err := mm.RunModule(moduleName, false)
	if err != nil {
		t.Fatal(err)
	}

	module, err := mm.GetModule(moduleName)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(expectedModuleValues, module.values()) {
		t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectedModuleValues, module.values())
	}

	assert.Equal(t, hc.DeleteSingleFailedRevisionExecuted, true, "helm.DeleteSingleFailedRevision must be executed!")
	assert.Equal(t, hc.UpgradeReleaseExecuted, true, "helm.UpgradeReleaseExecuted must be executed!")
}

func Test_MainModuleManager_DeleteModule(t *testing.T) {
	// TODO check afterHelmDelete patch
	t.SkipNow()
	hc := &helm.MockHelmClient{}
	helm.Client = hc

	mm := NewMainModuleManager()
	mm.WithKubeConfigManager(MockKubeConfigManager{})

	initModuleManager(t, mm, "test_delete_module")

	moduleName := "module"
	expectedModuleValues := utils.Values{
		"global": map[string]interface{}{
			"enabledModules": []string{},
		},
		"module": map[string]interface{}{
			"afterDeleteHelm": "override-value",
			"replicaCount":    1.0,
			"image": map[string]interface{}{
				"repository": "nginx",
				"tag":        "stable",
				"pullPolicy": "IfNotPresent",
			},
		},
	}

	err := mm.DeleteModule(moduleName)
	if err != nil {
		t.Fatal(err)
	}

	module, err := mm.GetModule(moduleName)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(expectedModuleValues, module.values()) {
		t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectedModuleValues, module.values())
	}

	assert.Equal(t, hc.DeleteReleaseExecuted, true, "helm.DeleteRelease must be executed!")
}

func Test_MainModuleManager_RunModuleHook(t *testing.T) {
	// TODO hooks not found
	t.SkipNow()
	helm.Client = &helm.MockHelmClient{}
	mm := NewMainModuleManager()
	mm.WithKubeConfigManager(MockKubeConfigManager{})

	initModuleManager(t, mm, "test_run_module_hook")

	expectations := []struct {
		testName                   string
		moduleName                 string
		hookName                   string
		kubeModuleConfigValues     utils.Values
		moduleDynamicValuesPatches []utils.ValuesPatch
		expectedModuleConfigValues utils.Values
		expectedModuleValues       utils.Values
	}{
		{
			"merge_and_patch_kube_module_config_values",
			"update-kube-module-config",
			"000-update-kube-module-config/hooks/merge_and_patch_values",
			utils.Values{
				"updateKubeModuleConfig": map[string]interface{}{
					"b": "should-be-deleted",
				},
			},
			[]utils.ValuesPatch{},
			utils.Values{
				"global": map[string]interface{}{},
				"updateKubeModuleConfig": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0},
				},
			},
			utils.Values{
				"global": map[string]interface{}{
					"enabledModules": []string{},
				},
				"updateKubeModuleConfig": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0},
				},
			},
		},
		{
			"merge_and_patch_module_dynamic_values",
			"update-module-dynamic",
			"100-update-module-dynamic/hooks/merge_and_patch_values",
			utils.Values{},
			[]utils.ValuesPatch{},
			utils.Values{
				"global":              map[string]interface{}{},
				"updateModuleDynamic": map[string]interface{}{},
			},
			utils.Values{
				"global": map[string]interface{}{
					"enabledModules": []string{},
				},
				"updateModuleDynamic": map[string]interface{}{
					"a": 9.0, "c": "10",
				},
			},
		},
		{
			"merge_and_patch_over_existing_kube_module_config_values",
			"update-kube-module-config",
			"000-update-kube-module-config/hooks/merge_and_patch_values",
			utils.Values{
				"updateKubeModuleConfig": map[string]interface{}{
					"a": 1.0, "b": 2.0, "x": "123",
				},
			},
			[]utils.ValuesPatch{},
			utils.Values{
				"global": map[string]interface{}{},
				"updateKubeModuleConfig": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0}, "x": "123",
				},
			},
			utils.Values{
				"global": map[string]interface{}{
					"enabledModules": []string{},
				},
				"updateKubeModuleConfig": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0}, "x": "123",
				},
			},
		},
		{
			"merge_and_patch_over_existing_module_dynamic_values",
			"update-module-dynamic",
			"100-update-module-dynamic/hooks/merge_and_patch_values",
			utils.Values{},
			[]utils.ValuesPatch{
				*utils.MustValuesPatch(utils.ValuesPatchFromBytes([]byte(`[
{"op": "add", "path": "/updateModuleDynamic/a", "value": 123},
{"op": "add", "path": "/updateModuleDynamic/x", "value": 10}
				]`))),
			},
			utils.Values{
				"global":              map[string]interface{}{},
				"updateModuleDynamic": map[string]interface{}{},
			},
			utils.Values{
				"global": map[string]interface{}{
					"enabledModules": []string{},
				},
				"updateModuleDynamic": map[string]interface{}{
					"a": 9.0, "c": "10", "x": 10.0,
				},
			},
		},
	}

	mm.kubeModulesConfigValues = make(map[string]utils.Values)
	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			mm.kubeModulesConfigValues[expectation.moduleName] = expectation.kubeModuleConfigValues
			mm.modulesDynamicValuesPatches[expectation.moduleName] = expectation.moduleDynamicValuesPatches

			if err := mm.RunModuleHook(expectation.hookName, BeforeHelm, nil); err != nil {
				t.Fatal(err)
			}

			module, err := mm.GetModule(expectation.moduleName)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(expectation.expectedModuleConfigValues, module.configValues()) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedModuleConfigValues, module.configValues())
			}

			if !reflect.DeepEqual(expectation.expectedModuleValues, module.values()) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedModuleValues, module.values())
			}
		})
	}
}

//func Test_MainModuleManager_Get_GlobalHook(t *testing.T) {
//	mm := NewMainModuleManager()
//
//	initModuleManager(t, mm, "get__global_hook")
//
//	var globalHook *GlobalHook
//	var err error
//
//	tests := []struct {
//		name              string
//		hookName          string
//		testFn func()
//	}{
//		{
//			"global-hook-with-all-bindings",
//			"000-all-bindings/all",
//			func() {
//				expectedHook := &GlobalHook{
//					&CommonHook{
//						"000-all-bindings/all",
//						filepath.Join(mm.GlobalHooksDir, "000-all-bindings/all"),
//						[]BindingType{BeforeAll, AfterAll, OnStartup, Schedule, KubeEvents},
//						map[BindingType]float64{
//							BeforeAll: 1.0,
//							AfterAll:  1.0,
//							OnStartup: 1.0,
//						},
//						mm,
//					},
//					&GlobalHookConfig{
//						HookConfig{
//							1.0,
//							[]schedule_manager.ScheduleConfig{
//								{
//									Crontab:      "* * * * *",
//									AllowFailure: true,
//								},
//							},
//							[]kube_events_manager.OnKubernetesEventConfig{
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{kube_events_manager.KubernetesEventOnAdd},
//									Kind:       "configmap",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component1",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: []string{"namespace1"},
//										Any:        false,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field1)",
//									AllowFailure: true,
//								},
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{
//										kube_events_manager.KubernetesEventOnAdd,
//										kube_events_manager.KubernetesEventOnUpdate,
//										kube_events_manager.KubernetesEventOnDelete,
//									},
//									Kind: "namespace",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component2",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: []string{"namespace2"},
//										Any:        false,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field2)",
//									AllowFailure: true,
//								},
//								{
//									EventTypes: []kube_events_manager.OnKubernetesEventType{
//										kube_events_manager.KubernetesEventOnAdd,
//										kube_events_manager.KubernetesEventOnUpdate,
//										kube_events_manager.KubernetesEventOnDelete,
//									},
//									Kind: "pod",
//									Selector: &metav1.LabelSelector{
//										MatchLabels: map[string]string{
//											"component": "component3",
//										},
//										MatchExpressions: []metav1.LabelSelectorRequirement{
//											{
//												Key:      "tier",
//												Operator: "In",
//												Values:   []string{"cache"},
//											},
//										},
//									},
//									NamespaceSelector: &kube_events_manager.KubeNamespaceSelector{
//										MatchNames: nil,
//										Any:        true,
//									},
//									JqFilter:     ".items[] | del(.metadata, .field3)",
//									AllowFailure: true,
//								},
//							},
//						},
//						1.0,
//						1.0,
//					},
//				}
//
//				if assert.NoError(t, err) {
//					assert.Equal(t, expectedHook, globalHook)
//				}
//			},
//		},
//		{
//			"global-hook-nested",
//			"100-nested-hook/sub/sub/nested-before-all",
//			func() {
//				expectedHook := &GlobalHook{
//					&CommonHook {
//						"100-nested-hook/sub/sub/nested-before-all",
//						filepath.Join(mm.GlobalHooksDir, "100-nested-hook/sub/sub/nested-before-all"),
//						[]BindingType{BeforeAll},
//						map[BindingType]float64{
//							BeforeAll: 1.0,
//						},
//						mm,
//					},
//					&GlobalHookConfig{
//						HookConfig{
//							nil,
//							nil,
//							nil,
//						},
//						1.0,
//						nil,
//					},
//				}
//				if assert.NoError(t, err) {
//					assert.Equal(t, expectedHook, globalHook)
//				}
//			},
//		},
//		{
//			"error-if-hook-not-registered",
//			"non-existent",
//			func(){
//				assert.Error(t, err)
//				assert.Nil(t, globalHook)
//			},
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			globalHook, err = mm.GetGlobalHook(test.hookName)
//			test.testFn()
//		})
//	}
//}

func Test_MainModuleManager_Get_GlobalHooksInOrder(t *testing.T) {
	helm.Client = &helm.MockHelmClient{}
	mm := NewMainModuleManager()

	initModuleManager(t, mm, "get__global_hooks_in_order")

	var expectations = []struct {
		testName    string
		bindingType BindingType
		hooksOrder  []string
	}{
		{
			testName:    "after-all-hooks-in-order",
			bindingType: AfterAll,
			hooksOrder: []string{
				"000-before-all-binding-hooks/b",
				"000-before-all-binding-hooks/c",
				"000-before-all-binding-hooks/a",
			},
		},
		{
			testName:    "before-helm-binding-type-no-error",
			bindingType: BeforeHelm,
			hooksOrder:  []string{},
		},
	}

	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			globalHooks := mm.GetGlobalHooksInOrder(expectation.bindingType)
			assert.Equal(t, expectation.hooksOrder, globalHooks)
		})
	}
}

func Test_MainModuleManager_Run_GlobalHook(t *testing.T) {
	helm.Client = &helm.MockHelmClient{}
	mm := NewMainModuleManager()
	mm.WithKubeConfigManager(MockKubeConfigManager{})

	initModuleManager(t, mm, "test_run_global_hook")

	expectations := []struct {
		testName                   string
		hookName                   string
		kubeGlobalConfigValues     utils.Values
		globalDynamicValuesPatches []utils.ValuesPatch
		expectedConfigValues       utils.Values
		expectedValues             utils.Values
	}{
		{
			"merge_and_patch_kube_config_values",
			"000-update-kube-config/merge_and_patch_values",
			utils.Values{
				"global": map[string]interface{}{
					"b": "should-be-deleted",
				},
			},
			[]utils.ValuesPatch{},
			utils.Values{
				"global": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0},
				},
			},
			utils.Values{
				"global": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0},
				},
			},
		},
		{
			"merge_and_patch_dynamic_values",
			"100-update-dynamic/merge_and_patch_values",
			utils.Values{},
			[]utils.ValuesPatch{},
			utils.Values{
				"global": map[string]interface{}{},
			},
			utils.Values{
				"global": map[string]interface{}{
					"a": 9.0, "c": "10",
				},
			},
		},
		{
			"merge_and_patch_over_existing_kube_config_values",
			"000-update-kube-config/merge_and_patch_values",
			utils.Values{
				"global": map[string]interface{}{
					"a": 1.0, "b": 2.0, "x": "123",
				},
			},
			[]utils.ValuesPatch{},
			utils.Values{
				"global": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0}, "x": "123",
				},
			},
			utils.Values{
				"global": map[string]interface{}{
					"a": 2.0, "c": []interface{}{3.0}, "x": "123",
				},
			},
		},
		{
			"merge_and_patch_over_existing_dynamic_values",
			"100-update-dynamic/merge_and_patch_values",
			utils.Values{},
			[]utils.ValuesPatch{
				*utils.MustValuesPatch(utils.ValuesPatchFromBytes([]byte(`[
{"op": "add", "path": "/global/a", "value": 123},
{"op": "add", "path": "/global/x", "value": 10.0}
				]`))),
			},
			utils.Values{
				"global": map[string]interface{}{},
			},
			utils.Values{
				"global": map[string]interface{}{
					"a": 9.0, "c": "10", "x": 10.0,
				},
			},
		},
	}

	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			mm.kubeGlobalConfigValues = expectation.kubeGlobalConfigValues
			mm.globalDynamicValuesPatches = expectation.globalDynamicValuesPatches

			if err := mm.RunGlobalHook(expectation.hookName, BeforeHelm, []BindingContext{}); err != nil {
				t.Fatal(err)
			}

			hook, err := mm.GetGlobalHook(expectation.hookName)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(expectation.expectedConfigValues, hook.configValues()) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedConfigValues, hook.configValues())
			}

			if !reflect.DeepEqual(expectation.expectedValues, hook.values()) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedValues, hook.values())
			}
		})
	}
}


func Test_MainModuleManager_DiscoverModulesState(t *testing.T) {

	var mm *MainModuleManager
	var modulesState *ModulesState
	var err error

	tests := []struct{
		name string
		configPath string
		helmReleases []string
		testFn func()
	} {
		{
			"static_config_and_helm_releases",
			"discover_modules_state__simple",
			[]string{"module-1", "module-2", "module-3", "module-5", "module-6", "module-9"},
			func() {
				if assert.NoError(t, err) {
					assert.Equal(t, []string{"module-1", "module-4", "module-8"}, mm.enabledModulesByConfig)
					assert.Equal(t, []string{"module-6", "module-5", "module-2"}, modulesState.ReleasedUnknownModules)
					assert.Equal(t, []string{"module-9", "module-3"}, modulesState.ModulesToDisable)
				}
			},
		},
		{
			"enabled_script",
			"discover_modules_state__with_enabled_scripts",
			[]string{},
			func() {
				// if all modules are enabled by default, then beta should be disabled by script
				assert.Equal(t, []string{"alpha", "gamma", "delta", "epsilon", "zeta", "eta"}, modulesState.EnabledModules)

				// turn off alpha so gamma, delta and zeta should be disabled
				// with the next call of DiscoverModulesState
				mm.enabledModulesByConfig = []string{"beta", "gamma", "delta", "epsilon", "zeta", "eta"}
				modulesState, err = mm.DiscoverModulesState()
				assert.Equal(t, []string{"epsilon", "eta"}, modulesState.EnabledModules)
			},
		},
		{
			"module_names_in_order",
			"discover_modules_state__module_names_order",
			[]string{},
			func() {
				expectedModules := []string{
					"module-c",
					"module-b",
				}
				// if all modules are enabled by default, then beta should be disabled by script
				assert.Equal(t, expectedModules, modulesState.EnabledModules)

			},
		},
	}


	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modulesState = nil
			err = nil

			helm.Client = &helm.MockHelmClient{
				ReleaseNames: test.helmReleases,
			}
			mm = NewMainModuleManager()
			initModuleManager(t, mm, test.configPath)

			modulesState, err = mm.DiscoverModulesState()

			test.testFn()
		})
	}

}
