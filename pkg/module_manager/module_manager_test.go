package module_manager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/shell-operator/pkg/unilogger"
)

func TestLoadModules(t *testing.T) {
	cfg := &ModuleManagerConfig{
		DirectoryConfig: DirectoryConfig{
			ModulesDir:     "./testdata/loader",
			GlobalHooksDir: "./testdata/loader",
		},
		Dependencies: ModuleManagerDependencies{},
	}
	mm := NewModuleManager(context.Background(), cfg, unilogger.NewNop())
	gv, err := mm.loadGlobalValues()
	require.NoError(t, err)

	assert.YAMLEq(t, `
modules:
  ingressClass: nginx
  placement: {}
  https:
    mode: CertManager
    certManager:
      clusterIssuerName: letsencrypt
  resourcesRequests:
    everyNode:
      cpu: 300m
      memory: 512Mi
`, gv.globalValues.AsString("yaml"))

	assert.Equal(t, map[string]struct{}{
		"admission-policy-engine": {},
		"cert-manager":            {},
		"chrony":                  {},
		"cloud-data-crd":          {},
		"control-plane-manager":   {},
		"dashboard":               {},
		"deckhouse":               {},
	}, gv.enabledModules)
}

// TODO: these tests are about values tests only. We have to restore part of them

/*
import (
	"context"
	"fmt"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	mockhelm "github.com/flant/addon-operator/pkg/helm/test/mock"
	mockhelmresmgr "github.com/flant/addon-operator/pkg/helm_resources_manager/test/mock"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/backend/configmap"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	_ "github.com/flant/addon-operator/pkg/module_manager/test/go_hooks/global-hooks"
	"github.com/flant/addon-operator/pkg/utils"
	klient "github.com/flant/kube-client/client"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

type TestKubeConfigManager interface {
	Init() error
	Start()
	Stop()
	KubeConfigEventCh() chan config.KubeConfigEvent
	SafeReadConfig(handler func(config *config.KubeConfig))
}

type initModuleManagerResult struct {
	moduleManager        *ModuleManager
	kubeConfigManager    TestKubeConfigManager
	helmClient           *mockhelm.Client
	helmResourcesManager *mockhelmresmgr.MockHelmResourcesManager
	kubeClient           *klient.Client
	initialState         *ModulesState
	initialStateErr      error
	cmName               string
	cmNamespace          string
}

// initModuleManager creates a ready-to-use ModuleManager instance and some dependencies.
func initModuleManager(t *testing.T, configPath string) (*ModuleManager, *initModuleManagerResult) {
	const defaultNamespace = "default"
	const defaultName = "addon-operator"

	result := new(initModuleManagerResult)

	// Mock helm client for module.go, hook_executor.go
	result.helmClient = &mockhelm.Client{}

	// Mock helm resources manager to execute module actions: run, delete.
	result.helmResourcesManager = &mockhelmresmgr.MockHelmResourcesManager{}

	// Init directories
	rootDir := filepath.Join("testdata", configPath)

	var err error

	{
		// Load config values from config_map.yaml.
		cmFilePath := filepath.Join(rootDir, "config_map.yaml")
		cmExists, _ := utils_file.FileExists(cmFilePath)
		var cmObj *v1.ConfigMap
		if cmExists {
			cmDataBytes, err := os.ReadFile(cmFilePath)
			require.NoError(t, err, "Should read config map file '%s'", cmFilePath)

			cmObj = new(v1.ConfigMap)
			err = yaml.Unmarshal(cmDataBytes, &cmObj)
			require.NoError(t, err, "Should parse YAML in %s", cmFilePath)
			if cmObj.Namespace == "" {
				cmObj.SetNamespace(defaultNamespace)
			}
		} else {
			cmObj = &v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultName,
					Namespace: defaultNamespace,
				},
				Data: nil,
			}
		}
		result.cmName = cmObj.Name
		result.cmNamespace = cmObj.Namespace
		result.kubeClient = klient.NewFake(nil)
		_, err = result.kubeClient.CoreV1().ConfigMaps(result.cmNamespace).Create(context.TODO(), cmObj, metav1.CreateOptions{})
		require.NoError(t, err, "Should create ConfigMap/%s", result.cmName)
	}

	bk := configmap.New(nil, result.kubeClient, result.cmNamespace, result.cmName)
	manager := kube_config_manager.NewKubeConfigManager(context.Background(), bk, nil)
	result.kubeConfigManager = manager

	err = result.kubeConfigManager.Init()
	require.NoError(t, err, "KubeConfigManager.Init should not fail")

	// Create and init moduleManager instance
	// Note: skip KubeEventManager, ScheduleManager, KubeObjectPatcher, MetricStorage, HookMetricStorage
	dirs := DirectoryConfig{
		ModulesDir:     filepath.Join(rootDir, "modules"),
		GlobalHooksDir: filepath.Join(rootDir, "global-hooks"),
		TempDir:        t.TempDir(),
	}
	deps := ModuleManagerDependencies{
		Helm:                 mockhelm.NewClientFactory(result.helmClient),
		HelmResourcesManager: result.helmResourcesManager,
		KubeConfigManager:    manager,
	}
	cfg := ModuleManagerConfig{
		DirectoryConfig: dirs,
		Dependencies:    deps,
	}
	result.moduleManager = NewModuleManager(context.Background(), &cfg)

	err = result.moduleManager.Init()
	require.NoError(t, err, "Should register global hooks and all modules")

	// Start KubeConfigManager to be able to change config values via patching ConfigMap.
	result.kubeConfigManager.Start()

	result.kubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
		result.initialState, result.initialStateErr = result.moduleManager.HandleNewKubeConfig(config)
	})

	return result.moduleManager, result
}

func Test_ModuleManager_LoadValuesInInit(t *testing.T) {
	var mm *ModuleManager

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
				assert.Equal(t, expectedValues, mm.commonStaticValues.Global(), "global section of common values")
			},
		},
		{
			"only_module_config",
			"load_values__module_static_only",
			func() {
				modWithValues1Expected := utils.Values{
					"withValues1": map[string]interface{}{
						"a": 1.0, "b": 2.0, "c": 3.0,
					},
				}

				modWithValues2Expected := utils.Values{
					"withValues2": map[string]interface{}{
						"a": []interface{}{1.0, 2.0, map[string]interface{}{"b": 3.0}},
					},
				}

				assert.True(t, mm.modules.Has("with-values-1"))
				assert.True(t, mm.modules.Has("with-values-2"))

				with1 := mm.modules.Get("with-values-1")
				assert.NotNil(t, with1.StaticConfig)
				assert.Equal(t, modWithValues1Expected, with1.StaticConfig.GetValues())

				with2 := mm.modules.Get("with-values-2")
				assert.NotNil(t, with2.StaticConfig)
				assert.Equal(t, modWithValues2Expected, with2.StaticConfig.GetValues())
			},
		},
		{
			// no values.yaml files, but modules are loaded properly
			"empty",
			"load_values__common_static_empty",
			func() {
				assert.Len(t, mm.commonStaticValues, 0)
				assert.Len(t, mm.commonStaticValues.Global(), 0)
				assert.Len(t, mm.modules.List(), 1)
				assert.NotNil(t, mm.modules.Get("module").CommonStaticConfig)
				assert.NotNil(t, mm.modules.Get("module").StaticConfig)
			},
		},
		{
			"mixed",
			"load_values__common_and_module_and_kube",
			func() {
				assert.Len(t, mm.commonStaticValues, 4)
				assert.Len(t, mm.commonStaticValues.Global(), 1)
				assert.Len(t, mm.modules.List(), 4)

				assert.True(t, mm.modules.Has("with-values-1"))
				assert.True(t, mm.modules.Has("with-values-2"))
				assert.True(t, mm.modules.Has("without-values"))
				assert.True(t, mm.modules.Has("with-kube-values"))

				with1 := mm.modules.Get("with-values-1")
				assert.NotNil(t, with1.CommonStaticConfig)
				assert.NotNil(t, with1.StaticConfig)
				assert.Equal(t, "with-values-1", with1.CommonStaticConfig.ModuleName)
				assert.Equal(t, "withValues1", with1.CommonStaticConfig.ModuleConfigKey())
				assert.Equal(t, "withValues1Enabled", with1.CommonStaticConfig.ModuleEnabledKey())
				assert.Equal(t, "with-values-1", with1.StaticConfig.ModuleName)

				// with-values-1 is enabled by common values.yaml
				assert.True(t, *with1.CommonStaticConfig.IsEnabled)
				assert.False(t, *with1.StaticConfig.IsEnabled)

				assert.Len(t, with1.CommonStaticConfig.GetValues()["withValues1"], 1)
				assert.Len(t, with1.StaticConfig.GetValues()["withValues1"], 3)
				// with-values-1 has "a" value in common and in module static values
				assert.Contains(t, with1.CommonStaticConfig.GetValues()["withValues1"], "a")
				assert.Contains(t, with1.StaticConfig.GetValues()["withValues1"], "a")

				assert.NotContains(t, mm.kubeModulesConfigValues, "with-values-1")

				// all modules should have CommonStaticConfig and StaticConfig
				assert.NotNil(t, mm.modules.Get("with-values-2").CommonStaticConfig)
				assert.NotNil(t, mm.modules.Get("with-values-2").StaticConfig)
				assert.NotNil(t, mm.modules.Get("without-values").CommonStaticConfig)
				assert.NotNil(t, mm.modules.Get("without-values").StaticConfig)
				assert.NotNil(t, mm.modules.Get("with-kube-values").CommonStaticConfig)
				assert.NotNil(t, mm.modules.Get("with-kube-values").StaticConfig)

				fmt.Printf("kubeModulesConfigValues: %#v\n", mm.kubeModulesConfigValues)

				// with-values-2 module is disabled but config values should be loaded.
				assert.Contains(t, mm.kubeModulesConfigValues, "with-values-2")
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
			_, res := initModuleManager(t, test.configPath)
			mm = res.moduleManager
			test.testFn()
		})
	}
}

func Test_ModuleManager_LoadValues_ApplyDefaults(t *testing.T) {
	_, res := initModuleManager(t, "load_values__module_apply_defaults")

	// assert.Len(t, mm.commonStaticValues, 1)
	// assert.Len(t, mm.commonStaticValues.Global(), 1)
	assert.Len(t, res.moduleManager.modules.List(), 1)

	assert.True(t, res.moduleManager.modules.Has("module-one"))

	modOne := res.moduleManager.modules.Get("module-one")
	assert.NotNil(t, modOne.CommonStaticConfig)
	assert.NotNil(t, modOne.StaticConfig)
	assert.Equal(t, "module-one", modOne.CommonStaticConfig.ModuleName)
	assert.Equal(t, "module-one", modOne.StaticConfig.ModuleName)
	assert.Equal(t, "moduleOne", modOne.CommonStaticConfig.ModuleConfigKey())
	assert.Equal(t, "moduleOneEnabled", modOne.CommonStaticConfig.ModuleEnabledKey())

	// module-one is not enabled in any of values.yaml
	assert.Nil(t, modOne.CommonStaticConfig.IsEnabled)
	assert.Nil(t, modOne.StaticConfig.IsEnabled)

	assert.Contains(t, res.moduleManager.kubeModulesConfigValues, "module-one")

	vals, err := modOne.Values()
	assert.Nil(t, err)

	assert.Contains(t, vals, modOne.ValuesKey())
	modVals := vals[modOne.ValuesKey()].(map[string]interface{})
	assert.Contains(t, modVals, "internal")
	internalVals := modVals["internal"].(map[string]interface{})
	assert.Contains(t, internalVals, "param1")
	unk := internalVals["param1"].(string)
	assert.Equal(t, "unknown", unk)

	// Also check global defaults
	assert.Contains(t, vals, utils.GlobalValuesKey)
	globVals := vals[utils.GlobalValuesKey].(map[string]interface{})
	assert.Contains(t, globVals, "grafana")
	graf := globVals["grafana"].(string)
	assert.Equal(t, "grafana", graf)

	// 'testvalue1' field from modules/values.yaml.
	assert.Contains(t, globVals, "init")
	initVals := globVals["init"].(map[string]interface{})
	assert.Contains(t, initVals, "param1")

	// 'discovery' field default from values.yaml schema.
	assert.Contains(t, globVals, "discovery")
}

func Test_ModuleManager_Get_Module(t *testing.T) {
	mm, res := initModuleManager(t, "get__module")

	programmaticModule := &Module{Name: "programmatic-module", Order: 10}
	res.moduleManager.modules.Add(programmaticModule)

	var module *Module

	tests := []struct {
		name       string
		moduleName string
		testFn     func(t *testing.T)
	}{
		{
			"module_loaded_from_files",
			"module",
			func(t *testing.T) {
				require.NotNil(t, module)
				require.Equal(t, "module", module.Name)
				require.Equal(t, filepath.Join(res.moduleManager.ModulesDir, "000-module"), module.Path)
				require.NotNil(t, module.CommonStaticConfig)
				require.Nil(t, module.CommonStaticConfig.IsEnabled)
				require.NotNil(t, module.StaticConfig)
				require.Nil(t, module.StaticConfig.IsEnabled)
				require.NotNil(t, module.State)
				require.NotNil(t, module.moduleManager)
			},
		},
		{
			"direct_add_module_to_index",
			"programmatic-module",
			func(t *testing.T) {
				require.Equal(t, programmaticModule, module)
			},
		},
		{
			"error-on-non-existent-module",
			"non-existent",
			func(t *testing.T) {
				require.Nil(t, module)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module = nil
			module = mm.GetModule(test.moduleName)
			test.testFn(t)
		})
	}
}

// Modules in get__module_hook path:
// - 000-all-bindings module with all-bindings hook with all possible bindings
// - 100-nested-hooks with hook in deep subdirectory.
func Test_ModuleManager_Get_ModuleHook(t *testing.T) {
	mm, _ := initModuleManager(t, "get__module_hook")

	// Register modules hooks.
	for _, modName := range []string{"all-bindings", "nested-hooks"} {
		err := mm.RegisterModuleHooks(mm.GetModule(modName), map[string]string{})
		require.NoError(t, err, "Should register hooks for module '%s'", modName)
	}

	var moduleHook *ModuleHook

	tests := []struct {
		name     string
		hookName string
		testFn   func(t *testing.T)
	}{
		{
			"module-hook-all-bindings",
			"000-all-bindings/hooks/all-bindings",
			func(t *testing.T) {
				require.NotNil(t, moduleHook, "Module hook 'all-bindings' should be registered")

				assert.Equal(t, "000-all-bindings/hooks/all-bindings", moduleHook.Name)
				assert.NotNil(t, moduleHook.Config)
				assert.Equal(t, []BindingType{OnStartup, Schedule, OnKubernetesEvent, BeforeHelm, AfterHelm, AfterDeleteHelm}, moduleHook.Config.Bindings())
				assert.Len(t, moduleHook.Config.OnKubernetesEvents, 3, "Should register 3 kubernetes bindings")
				assert.Len(t, moduleHook.Config.Schedules, 1, "Should register 1 schedule binding")

				// Schedule binding is 'every minute' with allow failure.
				schBinding := moduleHook.Config.Schedules[0]
				assert.Equal(t, "* * * * *", schBinding.ScheduleEntry.Crontab)
				assert.True(t, schBinding.AllowFailure)

				kBinding := moduleHook.Config.OnKubernetesEvents[0]
				assert.True(t, kBinding.AllowFailure)
				assert.NotNil(t, kBinding.Monitor)
				assert.NotNil(t, kBinding.Monitor.NamespaceSelector)
				assert.NotNil(t, kBinding.Monitor.LabelSelector)
				assert.Nil(t, kBinding.Monitor.NameSelector)
				assert.Nil(t, kBinding.Monitor.FieldSelector)
				assert.Equal(t, "configmap", kBinding.Monitor.Kind)
				assert.Equal(t, []types.WatchEventType{types.WatchEventAdded}, kBinding.Monitor.EventTypes)

				// Binding without executeHookOnEvent should have all events
				kBinding = moduleHook.Config.OnKubernetesEvents[1]
				assert.True(t, kBinding.AllowFailure)
				assert.Equal(t, []types.WatchEventType{types.WatchEventAdded, types.WatchEventModified, types.WatchEventDeleted}, kBinding.Monitor.EventTypes)

				// Binding without allowFailure
				kBinding = moduleHook.Config.OnKubernetesEvents[2]
				assert.False(t, kBinding.AllowFailure)
			},
		},
		{
			"nested-module-hook",
			"100-nested-hooks/hooks/sub/sub/nested-before-helm",
			func(t *testing.T) {
				require.NotNil(t, moduleHook, "Module hook 'nested-before-helm' should be registered")
				assert.Equal(t, "100-nested-hooks/hooks/sub/sub/nested-before-helm", moduleHook.Name)
				assert.NotNil(t, moduleHook.Config)
				assert.Equal(t, []BindingType{BeforeHelm}, moduleHook.Config.Bindings())
			},
		},
		{
			"nil-on-non-existent-module-hook",
			"non-existent-hook-name",
			func(t *testing.T) {
				assert.Nil(t, moduleHook)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			moduleHook = mm.GetModuleHook(test.hookName)
			test.testFn(t)
		})
	}
}

func Test_ModuleManager_Get_ModuleHooksInOrder(t *testing.T) {
	mm, res := initModuleManager(t, "get__module_hooks_in_order")

	_, _ = mm.RefreshEnabledState(map[string]string{})

	// Register modules hooks.
	for _, modName := range []string{"after-helm-binding-hooks"} {
		err := mm.RegisterModuleHooks(mm.GetModule(modName), map[string]string{})
		require.NoError(t, err, "Should register hooks for module '%s'", modName)
	}

	var moduleHooks []string

	tests := []struct {
		name        string
		moduleName  string
		bindingType BindingType
		testFn      func()
	}{
		{
			"sorted-hooks",
			"after-helm-binding-hooks",
			AfterHelm,
			func() {
				// assert.Len(t, res.moduleManager.allModulesByName, 1)
				assert.Len(t, res.moduleManager.modulesHooksOrderByName, 1)

				expectedOrder := []string{
					"107-after-helm-binding-hooks/hooks/b",
					"107-after-helm-binding-hooks/hooks/c",
					"107-after-helm-binding-hooks/hooks/a",
				}

				assert.Equal(t, expectedOrder, moduleHooks)
			},
		},
		{
			"no-hooks-for-binding",
			"after-helm-binding-hooks",
			BeforeHelm,
			func() {
				assert.Equal(t, []string{}, moduleHooks)
			},
		},
		{
			"no-hooks-for-existent-module",
			"after-helm-binding-hookssss",
			BeforeHelm,
			func() {
				assert.Len(t, moduleHooks, 0)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			moduleHooks = nil
			moduleHooks = mm.GetModuleHooksInOrder(test.moduleName, test.bindingType)
			test.testFn()
		})
	}
}

// Path test_run_module contains only one module with beforeHelm and afterHelm hooks.
// This test runs module and checks resulting values.
func Test_ModuleManager_RunModule(t *testing.T) {
	const ModuleName = "module"
	var err error

	mm, res := initModuleManager(t, "test_run_module")

	module := mm.GetModule(ModuleName)
	require.NotNil(t, module, "Should get module %s", ModuleName)

	// Register module hooks.
	err = mm.RegisterModuleHooks(module, map[string]string{})
	require.NoError(t, err, "Should register module hooks")

	valuesChanged, err := mm.RunModule(ModuleName, map[string]string{})
	require.NoError(t, err, "Module %s should run successfully", ModuleName)
	require.True(t, valuesChanged, "Module hooks should change values")

	// Check values after running hooks:
	// global:
	//   enabledModules: []
	// module:
	//   afterHelm: "value-from-after-helm"
	//   beforeHelm: "value-from-before-helm-20"
	//   imageName: "nginx:stable"
	values, err := module.Values()
	require.NoError(t, err, "Should collect effective module values")
	{
		require.True(t, values.HasGlobal(), "Global values should present")

		// Check values contains section for "module" module.
		require.Contains(t, values, "module", "Should has module key in values")
		modVals, ok := values["module"].(map[string]interface{})
		require.True(t, ok, "value on module key should be map")
		{
			// Check value from hook-4.
			require.Contains(t, modVals, "afterHelm", "Should has afterHelm field")
			afterHelm, ok := modVals["afterHelm"].(string)
			require.True(t, ok, "afterHelm value should be string")
			require.Equal(t, afterHelm, "value-from-after-helm")

			// Check final value from hook-1, hook-2, and hook-3.
			require.Contains(t, modVals, "beforeHelm", "Should has beforeHelm field")
			beforeHelm, ok := modVals["beforeHelm"].(string)
			require.True(t, ok, "beforeHelm value should be string")
			require.Equal(t, beforeHelm, "value-from-before-helm-20", "beforeHelm should be from hook-1")

			// Check values from values.yaml.
			require.Contains(t, modVals, "imageName", "Should has imageName field from values.yaml")
			imageName, ok := modVals["imageName"].(string)
			require.True(t, ok, "imageName value should be string")
			require.Equal(t, "nginx:stable", imageName, "should have imageName value from values.yaml")
		}
	}

	// Check for helm client methods calls.
	assert.Equal(t, res.helmClient.UpgradeReleaseExecuted, true, "helm.UpgradeReleaseExecuted must be executed!")
}

// Path test_delete_module contains only one module with afterDeleteHelm hooks.
// This test runs module and checks resulting values.
func Test_ModuleManager_DeleteModule(t *testing.T) {
	const ModuleName = "module"
	var err error

	mm, res := initModuleManager(t, "test_delete_module")

	module := mm.GetModule(ModuleName)
	require.NotNil(t, module, "Should get module %s", ModuleName)

	// Register module hooks.
	err = mm.RegisterModuleHooks(module, map[string]string{})
	require.NoError(t, err, "Should register module hooks")

	err = mm.DeleteModule(ModuleName, map[string]string{})
	require.NoError(t, err, "Should delete module")

	// if !reflect.DeepEqual(expectedModuleValues, values) {
	//	t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectedModuleValues, values)
	//}
	// Check values after running hooks:
	// global:
	//   enabledModules: []
	// module:
	//   afterDeleteHelm: "value-from-after-delete-helm-20"
	//   imageName: "nginx:stable"
	values, err := module.Values()
	require.NoError(t, err, "Should collect effective module values")
	{
		require.True(t, values.HasGlobal(), "Global values should present")

		// Check values contains section for "module" module.
		require.Contains(t, values, "module", "Should has module key in values")
		modVals, ok := values["module"].(map[string]interface{})
		require.True(t, ok, "value on module key should be map")
		{
			// Check value from hook-1.
			require.Contains(t, modVals, "afterDeleteHelm", "Should has afterDeleteHelm field")
			afterHelm, ok := modVals["afterDeleteHelm"].(string)
			require.True(t, ok, "afterDeleteHelm value should be string")
			require.Equal(t, afterHelm, "value-from-after-delete-helm-20")

			// Check values from values.yaml.
			require.Contains(t, modVals, "imageName", "Should has imageName field from values.yaml")
			imageName, ok := modVals["imageName"].(string)
			require.True(t, ok, "imageName value should be string")
			require.Equal(t, "nginx:stable", imageName, "should have imageName value from values.yaml")
		}
	}

	assert.Equal(t, res.helmClient.DeleteReleaseExecuted, true, "helm.DeleteRelease must be executed!")
}

// Modules in test_run_module_hook path:
// - 000-update-kube-module-config with hook that add and remove some config values.
// - 000-update-module-dynamic with hook that add some dynamic values.
//
// Test runs these hooks and checks resulting values.
func Test_ModuleManager_RunModuleHook(t *testing.T) {
	mm, res := initModuleManager(t, "test_run_module_hook")

	// Register modules hooks.
	for _, modName := range []string{"update-kube-module-config", "update-module-dynamic"} {
		err := mm.RegisterModuleHooks(mm.GetModule(modName), map[string]string{})
		require.NoError(t, err, "Should register hooks for module '%s'", modName)
	}

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
			valuesPatchesFromYAML(t, `[
{"op": "add", "path": "/updateModuleDynamic/a", "value": 123},
{"op": "add", "path": "/updateModuleDynamic/x", "value": 10}
]`),
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

	res.moduleManager.kubeModulesConfigValues = make(map[string]utils.Values)
	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			res.moduleManager.kubeModulesConfigValues[expectation.moduleName] = expectation.kubeModuleConfigValues
			res.moduleManager.modulesDynamicValuesPatches[expectation.moduleName] = expectation.moduleDynamicValuesPatches

			_, _, err := mm.RunModuleHook(expectation.hookName, BeforeHelm, nil, map[string]string{})
			require.NoError(t, err, "Hook %s should not fail", expectation.hookName)

			module := mm.GetModule(expectation.moduleName)

			if !reflect.DeepEqual(expectation.expectedModuleConfigValues, module.ConfigValues()) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedModuleConfigValues, module.ConfigValues())
			}

			values, err := module.Values()
			require.NoError(t, err, "Should collect effective values for module")

			if !reflect.DeepEqual(expectation.expectedModuleValues, values) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedModuleValues, values)
			}
		})
	}
}

// Global hooks in test_run_global_hook path:
// - 000-update-kube-config hook that add and remove some config values.
// - 100-update-dynamic hook that add some dynamic values.
//
// Test checks hooks registration.
func Test_MainModuleManager_Get_GlobalHook(t *testing.T) {
	mm, _ := initModuleManager(t, "get__global_hook")

	var globalHook *hooks.GlobalHook

	tests := []struct {
		name     string
		hookName string
		testFn   func()
	}{
		{
			"global-hook-with-all-bindings",
			"000-all-bindings/hook",
			func() {
				require.NotNil(t, globalHook, "Global hook '000-all-bindings/hook' should be registered")

				assert.Equal(t, "000-all-bindings/hook", globalHook.Name)
				assert.NotNil(t, globalHook.Config)
				assert.Equal(t, []BindingType{OnStartup, Schedule, OnKubernetesEvent, BeforeAll, AfterAll}, globalHook.Config.Bindings())
				assert.Len(t, globalHook.Config.OnKubernetesEvents, 3, "Should register 3 kubernetes bindings")
				assert.Len(t, globalHook.Config.Schedules, 1, "Should register 1 schedule binding")

				// Schedule binding is 'every minute' with allow failure.
				schBinding := globalHook.Config.Schedules[0]
				assert.Equal(t, "* * * * *", schBinding.ScheduleEntry.Crontab)
				assert.True(t, schBinding.AllowFailure)

				kBinding := globalHook.Config.OnKubernetesEvents[0]
				assert.True(t, kBinding.AllowFailure)
				assert.NotNil(t, kBinding.Monitor)
				assert.NotNil(t, kBinding.Monitor.NamespaceSelector)
				assert.NotNil(t, kBinding.Monitor.LabelSelector)
				assert.Nil(t, kBinding.Monitor.NameSelector)
				assert.Nil(t, kBinding.Monitor.FieldSelector)
				assert.Equal(t, "configmap", kBinding.Monitor.Kind)
				assert.Equal(t, []types.WatchEventType{types.WatchEventAdded}, kBinding.Monitor.EventTypes)

				// Binding without executeHookOnEvent should have all events
				kBinding = globalHook.Config.OnKubernetesEvents[1]
				assert.True(t, kBinding.AllowFailure)
				assert.Equal(t, []types.WatchEventType{types.WatchEventAdded, types.WatchEventModified, types.WatchEventDeleted}, kBinding.Monitor.EventTypes)

				// Binding without allowFailure
				kBinding = globalHook.Config.OnKubernetesEvents[2]
				assert.False(t, kBinding.AllowFailure)
			},
		},
		{
			"global-hook-nested",
			"100-nested-hook/sub/sub/hook",
			func() {
				require.NotNil(t, globalHook, "Global hook '100-nested-hook/sub/sub/hook' should be registered")
				assert.Equal(t, "100-nested-hook/sub/sub/hook", globalHook.Name)
				assert.NotNil(t, globalHook.Config)
				assert.Equal(t, []BindingType{BeforeAll}, globalHook.Config.Bindings())
			},
		},
		{
			"nil-if-global-hook-not-registered",
			"non-existent-hook-name",
			func() {
				assert.Nil(t, globalHook)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			globalHook = mm.GetGlobalHook(test.hookName)
			test.testFn()
		})
	}
}

// This test checks sorting 'beforeAll' hooks in specified order.
func Test_ModuleManager_Get_GlobalHooksInOrder(t *testing.T) {
	mm, _ := initModuleManager(t, "get__global_hooks_in_order")

	expectations := []struct {
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
				"simple.go",
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

// Global hooks in test_run_global_hook path:
// - 000-update-kube-config hook that add and remove some config values.
// - 100-update-dynamic hook that add some dynamic values.
//
// Test runs global hooks and checks resulting values.
func Test_ModuleManager_Run_GlobalHook(t *testing.T) {
	mm, res := initModuleManager(t, "test_run_global_hook")

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
			valuesPatchesFromYAML(t, `[
{"op": "add", "path": "/global/a", "value": 123},
{"op": "add", "path": "/global/x", "value": 10.0}
]`),
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
			res.moduleManager.kubeGlobalConfigValues = expectation.kubeGlobalConfigValues
			res.moduleManager.globalDynamicValuesPatches = expectation.globalDynamicValuesPatches

			_, _, err := mm.RunGlobalHook(expectation.hookName, BeforeAll, []BindingContext{}, map[string]string{})
			require.NoError(t, err, "Hook %s should not fail", expectation.hookName)

			configValues := mm.GlobalConfigValues()
			if !reflect.DeepEqual(expectation.expectedConfigValues, configValues) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", spew.Sdump(expectation.expectedConfigValues), spew.Sdump(configValues))
			}

			values, err := mm.GlobalValues()
			require.NoError(t, err, "Should collect effective global values")
			if !reflect.DeepEqual(expectation.expectedValues, values) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", spew.Sdump(expectation.expectedValues), spew.Sdump(values))
			}
		})
	}
}

// Test modules in discover_modules_state_* paths:
// __simple - 6 modules , 2 modules should be disabled on startup.
// __with_enabled_scripts - check running enabled scripts.
// __module_names_order - check modules order.
func Test_ModuleManager_ModulesState_no_ConfigMap(t *testing.T) {
	var mm *ModuleManager
	var modulesState *ModulesState
	var err error

	tests := []struct {
		name         string
		configPath   string
		helmReleases []string
		testFn       func(t *testing.T)
	}{
		{
			"static_config_and_helm_releases",
			"modules_state__no_cm__simple",
			[]string{"module-1", "module-2", "module-3", "module-5", "module-6", "module-9"},
			func(t *testing.T) {
				// At start:
				// - module-1, module-4 and module-8 are enabled by values.yaml - they should be in 'enabledByConfig' list.
				// After RefreshFromHelmReleases:
				// - module-1, module-3 and module-9 are in releases, they should be in AllEnabled list.
				// - module-2, module-5, and module-6 are unknown, they should be in 'purge' list.
				// After RefreshEnabledState:
				// - module-1, module-4 and module-8 should be in AllEnabled list.
				// - module-4 and module-8 should be in 'enabled' list. (module-1 is already enabled as helm release)
				// - module-9 and module-3 should be in 'disabled' list. (releases are present, but modules are disabled)

				expectEnabledByConfig := map[string]struct{}{"module-1": {}, "module-4": {}, "module-8": {}}
				require.Equal(t, expectEnabledByConfig, mm.enabledModulesByConfig)

				expectAllEnabled := []string{"module-1", "module-3", "module-9"}
				// Note: purge in reversed order
				// expectToPurge := []string{"module-6", "module-5", "module-2"}
				require.Equal(t, expectAllEnabled, modulesState.AllEnabledModules)
				// require.Equal(t, expectToPurge, modulesState.ModulesToPurge)
				require.Len(t, modulesState.ModulesToEnable, 0)
				require.Len(t, modulesState.ModulesToDisable, 0)
				require.Len(t, modulesState.ModulesToReload, 0)

				modulesState, err := mm.RefreshEnabledState(map[string]string{})
				expectAllEnabled = []string{"module-1", "module-4", "module-8"}
				expectToEnable := []string{"module-4", "module-8"}
				// Note: 'disable' list should be in reverse order.
				expectToDisable := []string{"module-9", "module-3"}
				require.NoError(t, err, "Should refresh enabled state")
				require.Equal(t, expectAllEnabled, modulesState.AllEnabledModules)
				require.Equal(t, expectToEnable, modulesState.ModulesToEnable)
				require.Equal(t, expectToDisable, modulesState.ModulesToDisable)
			},
		},
		{
			"enabled_script",
			"modules_state__no_cm__with_enabled_scripts",
			[]string{},
			func(t *testing.T) {
				// At start:
				// - alpha, beta, gamma, delta, epsilon, zeta, eta are enabled by values.yaml - they should be in 'enabledByConfig' list.
				// After RefreshFromHelmReleases:
				// - No helm releases -> empty lists in modulesState.
				// After RefreshEnabledState:
				// - alpha enabled,
				// - beta disabled
				// - gamma requires alpha -> enabled
				// - delta requires alpha -> enabled
				// - epsilon enabled
				// - zeta requires delta and gamma -> enabled
				// - eta enabled
				// No beta in AllEnabled list.
				// After disable alpha with dynamicEnabled and RefreshEnabledState:
				// - all modules that depend on alpha should be disabled, so:
				// - epsilon and eta should be in AllEnabled list.

				expectEnabledByConfig := map[string]struct{}{
					"alpha":   {},
					"beta":    {},
					"gamma":   {},
					"delta":   {},
					"epsilon": {},
					"zeta":    {},
					"eta":     {},
				}

				require.Equal(t, expectEnabledByConfig, mm.enabledModulesByConfig, "All modules should be enabled by config at start")

				// No helm releases, modulesState is empty after RefreshFromHelmReleases.
				require.Len(t, modulesState.AllEnabledModules, 0)
				require.Len(t, modulesState.ModulesToPurge, 0)

				modulesState, err := mm.RefreshEnabledState(map[string]string{})
				require.NoError(t, err, "Should refresh enabled state")

				require.NotContains(t, modulesState.AllEnabledModules, "beta", "Should not return disabled 'beta' module")
				expectAllEnabled := []string{"alpha", "gamma", "delta", "epsilon", "zeta", "eta"}
				require.Equal(t, expectAllEnabled, modulesState.AllEnabledModules)

				// Turn off module 'alpha'.
				mm.dynamicEnabled["alpha"] = &utils.ModuleDisabled
				modulesState, err = mm.RefreshEnabledState(map[string]string{})
				require.NoError(t, err, "Should refresh enabled state")
				expectAllEnabled = []string{"epsilon", "eta"}
				require.Equal(t, expectAllEnabled, modulesState.AllEnabledModules)
				// Note: 'disable' list should be in reverse order.
				expectToDisable := []string{"zeta", "delta", "gamma", "alpha"}
				require.Equal(t, expectToDisable, modulesState.ModulesToDisable)
				require.Len(t, modulesState.ModulesToEnable, 0)
			},
		},
		{
			"module_names_in_order",
			"modules_state__no_cm__module_names_order",
			[]string{},
			func(t *testing.T) {
				// At start:
				// - module-c, module-b are enabled by config.
				// After RefreshEnabledState:
				// Enabled modules are not changed.
				expectEnabledByConfig := map[string]struct{}{
					"module-c": {},
					"module-b": {},
				}
				require.Equal(t, expectEnabledByConfig, mm.enabledModulesByConfig, "All modules should be enabled by config at start")

				// No helm releases, modulesState is empty after RefreshFromHelmReleases.
				require.Len(t, modulesState.AllEnabledModules, 0)
				require.Len(t, modulesState.ModulesToPurge, 0)

				modulesState, err := mm.RefreshEnabledState(map[string]string{})
				require.NoError(t, err, "Should refresh enabled state")

				expectAllEnabled := []string{
					"module-c",
					"module-b",
				}
				require.Equal(t, expectAllEnabled, modulesState.AllEnabledModules)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modulesState = nil
			err = nil

			_, res := initModuleManager(t, test.configPath)
			require.NoError(t, res.initialStateErr, "Should load ConfigMap state")
			mm = res.moduleManager

			mm.dependencies.Helm = mockhelm.NewClientFactory(&mockhelm.Client{
				ReleaseNames: test.helmReleases,
			})

			modulesState, err = mm.RefreshStateFromHelmReleases(map[string]string{})
			require.NoError(t, err, "Should refresh from helm releases")
			require.NotNil(t, modulesState, "Should have state after refresh from helm releases")
			test.testFn(t)
		})
	}
}

// Modules in modules_state__purge:
// - 'module-one' is enabled by common config.
// Initial state:
// - 'module-one', 'module-two', and 'module-three' are present as helm releases.
// - 'module-three' is disabled in ConfigMap.
// This test should detect 'module-two' as a helm release to purge and
// 'module-three' as a module to delete.
// 'module-one' should present in enabledModulesByConfig and enabledModules caches.
// 'module-three' should present in enabledModulesByConfig.
func Test_ModuleManager_ModulesState_detect_ConfigMap_changes(t *testing.T) {
	var state *ModulesState
	var err error

	mm, res := initModuleManager(t, "modules_state__detect_cm_changes")
	require.NoError(t, res.initialStateErr, "Should load initial config from ConfigMap")

	// enabledModulesByConfig is filled from values.yaml and ConfigMap data.
	require.Len(t, res.moduleManager.enabledModulesByConfig, 1)
	require.Contains(t, res.moduleManager.enabledModulesByConfig, "module-one")

	// RefreshStateFromHelmReleases should detect all modules from helm releases as enabled.
	mm.dependencies.Helm = mockhelm.NewClientFactory(&mockhelm.Client{
		ReleaseNames: []string{"module-one", "module-two", "module-three"},
	})
	state, err = mm.RefreshStateFromHelmReleases(map[string]string{})
	require.NoError(t, err, "RefreshStateFromHelmReleases should not fail")
	require.NotNil(t, state)
	require.Len(t, state.AllEnabledModules, 2)
	require.Contains(t, state.AllEnabledModules, "module-one")
	require.Contains(t, state.AllEnabledModules, "module-three")
	require.Equal(t, []string{}, state.ModulesToPurge)

	// No modules to enable, no modules to disable.
	state, err = mm.RefreshEnabledState(map[string]string{})
	require.NoError(t, err, "RefreshStateFromHelmReleases should not fail")
	require.NotNil(t, state)
	require.Equal(t, []string{"module-one"}, state.AllEnabledModules)
	require.Len(t, state.ModulesToEnable, 0)
	require.Len(t, state.ModulesToReload, 0)
	require.Len(t, state.ModulesToPurge, 0)
	require.Equal(t, []string{"module-three"}, state.ModulesToDisable)

	// Change ConfigMap: patch moduleThreeEnabled field, detect enabled module.
	{
		moduleThreeEnabledPatch := `
[{
"op": "replace",
"path": "/data/moduleThreeEnabled",
"value": "true"}]`

		_, err := res.kubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
			res.cmName,
			k8types.JSONPatchType,
			[]byte(moduleThreeEnabledPatch),
			metav1.PatchOptions{},
		)
		require.NoError(t, err, "ConfigMap should be patched")

		// Emulate ConvergeModules task: Wait for event, handle new ConfigMap, refresh enabled state.
		<-res.kubeConfigManager.KubeConfigEventCh()

		var state *ModulesState
		res.kubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			state, err = mm.HandleNewKubeConfig(config)
		})
		require.Len(t, state.ModulesToReload, 0, "Enabled flag change should lead to reload all modules")

		// module-one and module-three should be enabled.
		state, err = mm.RefreshEnabledState(map[string]string{})
		require.NoError(t, err, "Should refresh state")
		require.NotNil(t, state, "Should return state")
		require.Len(t, state.AllEnabledModules, 2)
		require.Contains(t, state.AllEnabledModules, "module-one")
		require.Contains(t, state.AllEnabledModules, "module-three")
		// module-three is a newly enabled module.
		require.Len(t, state.ModulesToEnable, 1)
		require.Contains(t, state.ModulesToEnable, "module-three")
	}

	// Change ConfigMap: patch moduleOne and moduleThree section, detect reload modules.
	{
		moduleValuesChangePatch := `
[{
"op": "replace",
"path": "/data/moduleOne",
"value": "param: newValue"},{
"op": "replace",
"path": "/data/moduleThree",
"value": "param: newValue"}]`

		_, err := res.kubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
			res.cmName,
			k8types.JSONPatchType,
			[]byte(moduleValuesChangePatch),
			metav1.PatchOptions{},
		)
		require.NoError(t, err, "ConfigMap should be patched")

		// Emulate ConvergeModules task: Wait for event, handle new ConfigMap, refresh enabled state.
		<-res.kubeConfigManager.KubeConfigEventCh()

		var state *ModulesState
		res.kubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			state, err = mm.HandleNewKubeConfig(config)
		})
		require.Len(t, state.ModulesToReload, 2, "Enabled flag change should lead to reload all modules")
		require.Contains(t, state.ModulesToReload, "module-one")
		require.Contains(t, state.ModulesToReload, "module-three")

		// module-one and module-three should be enabled.
		state, err = mm.RefreshEnabledState(map[string]string{})
		require.NoError(t, err, "Should refresh state")
		require.NotNil(t, state, "Should return state")
		require.Len(t, state.AllEnabledModules, 2)
		require.Contains(t, state.AllEnabledModules, "module-one")
		require.Contains(t, state.AllEnabledModules, "module-three")
		// Should be no changes in modules state.
		require.Len(t, state.ModulesToEnable, 0)
		require.Len(t, state.ModulesToDisable, 0)
		require.Len(t, state.ModulesToPurge, 0)
		require.Len(t, state.ModulesToReload, 0)
	}

	// Change ConfigMap: remove moduleThreeEnabled field, detect no changes, as module-three is enabled by values.yaml.
	{
		moduleThreeEnabledPatch := `
[{
"op": "remove",
"path": "/data/moduleThreeEnabled"}]`

		_, err := res.kubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
			res.cmName,
			k8types.JSONPatchType,
			[]byte(moduleThreeEnabledPatch),
			metav1.PatchOptions{},
		)
		require.NoError(t, err, "ConfigMap should be patched")

		// Emulate ConvergeModules task: Wait for event, handle new ConfigMap, refresh enabled state.
		<-res.kubeConfigManager.KubeConfigEventCh()

		var state *ModulesState
		res.kubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			state, err = mm.HandleNewKubeConfig(config)
		})
		require.NoError(t, err, "Should handle new ConfigMap")
		require.Nil(t, state, "Should be no changes in state from new ConfigMap")

		// module-one and module-three should still be enabled.
		state, err = mm.RefreshEnabledState(map[string]string{})
		require.NoError(t, err, "Should refresh state")
		require.NotNil(t, state, "Should return state")
		require.Len(t, state.AllEnabledModules, 2)
		require.Contains(t, state.AllEnabledModules, "module-one")
		require.Contains(t, state.AllEnabledModules, "module-three")
		// Should be no changes in modules state.
		require.Len(t, state.ModulesToEnable, 0)
		require.Len(t, state.ModulesToDisable, 0)
		require.Len(t, state.ModulesToPurge, 0)
		require.Len(t, state.ModulesToReload, 0)
	}
}

// initModuleManagerLight only loads modules and create ValuesValidator.
func initModuleManagerLight(t *testing.T, configPath string) *ModuleManager {
	// Init directories
	rootDir := filepath.Join("testdata", configPath)

	var err error

	// Create and init moduleManager instance
	// Note: skip KubeEventManager, ScheduleManager, KubeObjectPatcher, MetricStorage, HookMetricStorage
	dirs := DirectoryConfig{
		ModulesDir:     filepath.Join(rootDir, "modules"),
		GlobalHooksDir: filepath.Join(rootDir, "global"),
		TempDir:        t.TempDir(),
	}
	cfg := ModuleManagerConfig{
		DirectoryConfig: dirs,
	}
	moduleManager := NewModuleManager(context.Background(), &cfg)

	err = moduleManager.Init()
	require.NoError(t, err, "Should register global hooks and all modules")

	return moduleManager
}

// ModuleManager light mode: load global and modules and create ValuesValidator.
func Test_ModuleManager_Load_And_Validate(t *testing.T) {
	mm := initModuleManagerLight(t, "load_and_validate_usage")

	validGlobalValues := valuesFromYaml(t, `
global:
 paramStr: "val1"
 paramNum: 100
 paramBool: true
`)

	err := mm.GetValuesValidator().ValidateGlobalConfigValues(validGlobalValues)
	require.NoError(t, err, "should validate valid global values")

	invalidGlobalValues := valuesFromYaml(t, `
global:
 paramStr: 100
 paramNum: "100"
 paramBool: "yes"
`)

	err = mm.GetValuesValidator().ValidateGlobalConfigValues(invalidGlobalValues)
	require.Errorf(t, err, "should return error on invalid global values")

	validModuleValues := valuesFromYaml(t, `
testModule:
 paramStr: "val1"
 paramNum: 100
 paramBool: true
`)

	err = mm.GetValuesValidator().ValidateModuleConfigValues("testModule", validModuleValues)
	require.NoError(t, err, "should validate valid module values")

	invalidModuleValues := valuesFromYaml(t, `
testModule:
 paramStr: 100
 paramNum: "100"
 paramBool: "yes"
`)

	err = mm.GetValuesValidator().ValidateModuleConfigValues("testModule", invalidModuleValues)
	require.Errorf(t, err, "should return error on invalid module values")
}

func valuesFromYaml(t *testing.T, yamlStr string) utils.Values {
	vals, err := utils.NewValuesFromBytes([]byte(yamlStr))
	require.NoError(t, err, "should load values from string %s", yamlStr)
	return vals
}

func valuesPatchesFromYAML(t *testing.T, patches string) []utils.ValuesPatch {
	t.Helper()

	res, err := utils.ValuesPatchFromBytes([]byte(patches))
	require.NoError(t, err, "should load values patches from bytes, got err %v", err)

	return []utils.ValuesPatch{*res}
}
*/
