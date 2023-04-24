package addon_operator

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/config"
	"github.com/flant/shell-operator/pkg/debug"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

// Bootstrap inits all dependencies for a full-fledged AddonOperator instance.
func (op *AddonOperator) Bootstrap() error {
	runtimeConfig := config.NewConfig()
	// Init logging subsystem.
	sh_app.SetupLogging(runtimeConfig)
	log.Infof(sh_app.AppStartMessage)

	log.Infof("Search modules in: %s", app.ModulesDir)

	globalHooksDir, err := shell_operator.RequireExistingDirectory(app.GlobalHooksDir)
	if err != nil {
		log.Errorf("Fatal: global hooks directory: %s", err)
		return err
	}
	log.Infof("Global hooks directory: %s", globalHooksDir)

	tempDir, err := shell_operator.EnsureTempDirectory(sh_app.TempDir)
	if err != nil {
		log.Errorf("Fatal: temp directory: %s", err)
		return err
	}

	log.Infof("Addon-operator namespace: %s", app.Namespace)

	// Debug server.
	debugServer, err := shell_operator.InitDefaultDebugServer()
	if err != nil {
		log.Errorf("Fatal: start Debug server: %s", err)
		return err
	}

	err = shell_operator.AssembleCommonOperator(op.ShellOperator)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	err = op.Assemble(app.ModulesDir, globalHooksDir, tempDir, debugServer, runtimeConfig)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	err = op.CreateModuleCRD()
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	return nil
}

func (op *AddonOperator) Assemble(modulesDir string, globalHooksDir string, tempDir string, debugServer *debug.Server, runtimeConfig *config.Config) (err error) {
	RegisterDefaultRoutes(op)
	RegisterAddonOperatorMetrics(op.MetricStorage)
	StartLiveTicksUpdater(op.MetricStorage)
	StartTasksQueueLengthUpdater(op.MetricStorage, op.TaskQueues)

	// Register routes in debug server.
	shell_operator.RegisterDebugQueueRoutes(debugServer, op.ShellOperator)
	shell_operator.RegisterDebugConfigRoutes(debugServer, runtimeConfig)
	RegisterDebugGlobalRoutes(debugServer, op)
	RegisterDebugModuleRoutes(debugServer, op)

	// Helm client factory.
	op.Helm, err = helm.InitHelmClientFactory(op.KubeClient)
	if err != nil {
		return fmt.Errorf("initialize Helm: %s", err)
	}

	// Helm resources monitor.
	// It uses a separate client-go instance. (Metrics are registered when 'main' client is initialized).
	op.HelmResourcesManager, err = InitDefaultHelmResourcesManager(op.ctx, op.MetricStorage)
	if err != nil {
		return fmt.Errorf("initialize Helm resources manager: %s", err)
	}

	op.SetupModuleManager(modulesDir, globalHooksDir, tempDir, runtimeConfig)

	err = op.InitModuleManager()
	if err != nil {
		return err
	}

	return nil
}

func (op *AddonOperator) SetupModuleManager(modulesDir string, globalHooksDir string, tempDir string, runtimeConfig *config.Config) {
	// Create manager to check values in ConfigMap.
	kcfg := kube_config_manager.Config{
		Namespace:     app.Namespace,
		ConfigMapName: app.ConfigMapName,
		KubeClient:    op.KubeClient,
		RuntimeConfig: runtimeConfig,
	}
	manager := kube_config_manager.NewKubeConfigManager(op.ctx, &kcfg)
	op.KubeConfigManager = manager

	// Create manager that runs modules and hooks.
	dirConfig := module_manager.DirectoryConfig{
		ModulesDir:     modulesDir,
		GlobalHooksDir: globalHooksDir,
		TempDir:        tempDir,
	}
	cfg := module_manager.ModuleManagerDependencies{
		KubeObjectPatcher:    op.ObjectPatcher,
		KubeEventsManager:    op.KubeEventsManager,
		KubeConfigManager:    manager,
		ScheduleManager:      op.ScheduleManager,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		MetricStorage:        op.MetricStorage,
		HookMetricStorage:    op.HookMetricStorage,
	}
	op.ModuleManager = module_manager.NewModuleManager(op.ctx, dirConfig, &cfg)
}

func (op *AddonOperator) CreateModuleCRD() error {
	crd := generateCRD()
	return ensureCRD(op.ctx, op.KubeClient.Dynamic(), crd)
}

func generateCRD() *v1.CustomResourceDefinition {
	rd := v1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "modules.deckhouse.io",
		},
		Spec: v1.CustomResourceDefinitionSpec{
			Group: "deckhouse.io",
			Names: v1.CustomResourceDefinitionNames{
				Plural:   "modules",
				Singular: "module",
				Kind:     "Module",
			},
			Scope: "Cluster",
			Versions: []v1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &v1.CustomResourceValidation{
						OpenAPIV3Schema: &v1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]v1.JSONSchemaProps{
								"foo": {
									Type:    "string",
									Default: &v1.JSON{Raw: []byte("bar")},
								},
							},
						},
					},
					Subresources: &v1.CustomResourceSubresources{
						Status: &v1.CustomResourceSubresourceStatus{},
					},
					AdditionalPrinterColumns: nil,
				},
			},
			PreserveUnknownFields: false,
		},
		Status: v1.CustomResourceDefinitionStatus{},
	}

	return &rd
}

func ensureCRD(ctx context.Context, k8sDynamicClient dynamic.Interface, moduleCRD *v1.CustomResourceDefinition) error {
	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(moduleCRD)
	if err != nil {
		return err
	}

	moduleUnst := &unstructured.Unstructured{Object: content}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := k8sDynamicClient.Resource(gvr).Get(ctx, moduleCRD.ObjectMeta.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				_, err = k8sDynamicClient.Resource(gvr).Create(ctx, moduleUnst, metav1.CreateOptions{})
				if err != nil {
					return err
				}
			}
			return err
		}
		_, err = k8sDynamicClient.Resource(gvr).Update(ctx, moduleUnst, metav1.UpdateOptions{})
		return err
	})

	return retryErr
}
