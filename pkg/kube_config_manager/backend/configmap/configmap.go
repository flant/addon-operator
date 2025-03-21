package configmap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/client"
)

// Backend implements ConfigMap backend for kube_config_manager
type Backend struct {
	namespace string
	name      string

	logger *log.Logger
	client *client.Client
}

// New initializes backend for kube_config_manager based on ConfigMap with modules values
func New(kubeClient *client.Client, namespace, name string, logger *log.Logger) *Backend {
	if logger == nil {
		logger = log.NewLogger(log.Options{})
	}

	backend := &Backend{
		logger: logger.With("operator.component", "ConfigHandler").
			With("backend", "configmap"),
		namespace: namespace,
		name:      name,
		client:    kubeClient,
	}

	return backend
}

// LoadConfig gets config from ConfigMap before starting informer (selective loading configs for a list of modules isn't implemented).
// Set checksums for global section and modules.
func (b Backend) LoadConfig(ctx context.Context, _ ...string) (*config.KubeConfig, error) {
	obj, err := b.getConfigMap(ctx)
	if err != nil {
		return nil, err
	}

	if obj == nil {
		b.logger.Info("Initial config from ConfigMap: resource is not found",
			slog.String("name", b.name))
		return nil, nil
	}

	return parseConfigMapData(obj.Data)
}

// SaveConfigValues saves patches in the ConfigMap
func (b Backend) SaveConfigValues(ctx context.Context, key string, values utils.Values) ( /*checksum*/ string, error) {
	if key == utils.GlobalValuesKey {
		return b.saveGlobalConfigValues(ctx, values)
	}

	return b.saveModuleConfigValues(ctx, key, values)
}

func (b Backend) saveGlobalConfigValues(ctx context.Context, values utils.Values) ( /*checksum*/ string, error) {
	globalKubeConfig := &config.GlobalKubeConfig{
		Values:   values,
		Checksum: values.Checksum(),
	}

	if b.isDebugEnabled(ctx) {
		b.logger.Info("Save global values to ConfigMap",
			slog.String("name", b.name),
			slog.String("values", values.DebugString()))
	} else {
		b.logger.Info("Save global values to ConfigMap", slog.String("name", b.name))
	}

	err := b.mergeValues(ctx, "global", globalKubeConfig.GetValues())

	return globalKubeConfig.Checksum, err
}

func (b Backend) isDebugEnabled(ctx context.Context) bool {
	debug, ok := ctx.Value("kube-config-manager-debug").(bool)
	if !ok {
		return false
	}

	return debug
}

// saveModuleConfigValues updates module section in ConfigMap.
// It uses knownChecksums to prevent KubeConfigChanged event on self-update.
func (b Backend) saveModuleConfigValues(ctx context.Context, moduleName string, values utils.Values) ( /*checksum*/ string, error) {
	moduleKubeConfig := &config.ModuleKubeConfig{
		ModuleConfig: *utils.NewModuleConfig(moduleName, values),
		Checksum:     values.Checksum(),
	}

	if b.isDebugEnabled(ctx) {
		b.logger.Info("Save module values to ConfigMap",
			slog.String("moduleName", moduleName),
			slog.String("configMapName", b.name),
			slog.String("values", values.DebugString()))
	} else {
		b.logger.Info("Save module values to ConfigMap",
			slog.String("moduleName", moduleName),
			slog.String("configMapName", b.name))
	}

	err := b.mergeValues(ctx, utils.ModuleNameToValuesKey(moduleName), moduleKubeConfig.GetValues()) //nolint: staticcheck,nolintlint

	return moduleKubeConfig.Checksum, err
}

func (b Backend) getConfigMap(ctx context.Context) (*v1.ConfigMap, error) {
	obj, err := b.client.CoreV1().
		ConfigMaps(b.namespace).
		Get(ctx, b.name, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return obj, err
}

func parseConfigMapData(data map[string]string) (*config.KubeConfig, error) {
	var err error
	cfg := config.NewConfig()

	// Parse values in global section.
	cfg.Global, err = getGlobalKubeConfigFromConfigData(data)
	if err != nil {
		return nil, err
	}

	moduleNames, err := getModulesNamesFromConfigData(data)
	if err != nil {
		return nil, err
	}

	for moduleName := range moduleNames {
		cfg.Modules[moduleName], err = extractModuleKubeConfig(moduleName, data)
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func getGlobalKubeConfigFromConfigData(configData map[string]string) (*config.GlobalKubeConfig, error) {
	yamlData, hasKey := configData[utils.GlobalValuesKey]
	if !hasKey {
		return nil, nil
	}

	values, err := utils.NewGlobalValues(yamlData)
	if err != nil {
		return nil, fmt.Errorf("ConfigMap: bad yaml at key '%s': %s:\n%s", utils.GlobalValuesKey, err, yamlData)
	}

	checksum := values.Checksum()

	return &config.GlobalKubeConfig{
		Values:   values,
		Checksum: checksum,
	}, nil
}

// getModulesNamesFromConfigData returns all keys in kube config except global
// modNameEnabled keys are also handled
func getModulesNamesFromConfigData(configData map[string]string) (map[string]bool, error) {
	res := make(map[string]bool)

	for key := range configData {
		// Ignore global section.
		if key == utils.GlobalValuesKey {
			continue
		}

		// Treat Enabled and SelfService flags as module section.
		key = strings.TrimSuffix(key, utils.EnabledSuffix)
		key = strings.TrimSuffix(key, utils.SelfServiceSuffix)

		modName := utils.ModuleNameFromValuesKey(key)

		if utils.ModuleNameToValuesKey(modName) != key {
			return nil, fmt.Errorf("bad module name '%s': should be camelCased", key)
		}
		res[modName] = true
	}

	return res, nil
}

// extractModuleKubeConfig returns ModuleKubeConfig with values loaded from ConfigMap
func extractModuleKubeConfig(moduleName string, configData map[string]string) (*config.ModuleKubeConfig, error) {
	moduleConfig, err := fromConfigMapData(moduleName, configData)
	if err != nil {
		return nil, fmt.Errorf("bad yaml at key '%s': %s", utils.ModuleNameToValuesKey(moduleName), err)
	}
	// NOTE this should never happen because of GetModulesNamesFromConfigData
	if moduleConfig == nil {
		return nil, fmt.Errorf("possible bug!!! No section '%s' for module '%s'", utils.ModuleNameToValuesKey(moduleName), moduleName)
	}

	return &config.ModuleKubeConfig{
		ModuleConfig: *moduleConfig,
		Checksum:     moduleConfig.Checksum(),
	}, nil
}

// fromConfigMapData loads module config from a structure with string keys and yaml string values (ConfigMap)
//
// Example:
//
// simpleModule: |
//
//	param1: 10
//	param2: 120
//
// simpleModuleEnabled: "true"
func fromConfigMapData(moduleName string, configData map[string]string) (*utils.ModuleConfig, error) {
	mc := utils.NewModuleConfig(moduleName, nil)
	// create Values with moduleNameKey and moduleEnabled keys
	configValues := make(utils.Values)

	// if there is data for module, unmarshal it and put into configValues
	valuesYaml, hasKey := configData[mc.ModuleConfigKey()]
	if hasKey {
		var moduleValues interface{}

		err := yaml.Unmarshal([]byte(valuesYaml), &moduleValues)
		if err != nil {
			return nil, fmt.Errorf("unmarshal yaml data in a module config key '%s': %v", mc.ModuleConfigKey(), err)
		}

		configValues[mc.ModuleConfigKey()] = moduleValues
	}

	// if there is enabled or selfService keys, treat them as boolean
	enabledString, hasKey := configData[mc.ModuleEnabledKey()]
	if hasKey {
		var enabled bool

		switch enabledString {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			return nil, fmt.Errorf("module enabled key '%s' should have a boolean value, got '%v'", mc.ModuleEnabledKey(), enabledString)
		}

		configValues[mc.ModuleEnabledKey()] = enabled
	}

	selfServiceString, hasKey := configData[mc.ModuleSelfServiceKey()]
	if hasKey {
		var enabled bool

		switch selfServiceString {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			return nil, fmt.Errorf("module selfService key '%s' should have a boolean value, got '%v'", mc.ModuleSelfServiceKey(), selfServiceString)
		}

		configValues[mc.ModuleSelfServiceKey()] = enabled
	}

	if len(configValues) == 0 {
		return mc, nil
	}

	return mc.LoadFromValues(configValues)
}

func (b Backend) mergeValues(ctx context.Context, moduleName string, values utils.Values) error {
	return b.updateConfigMap(ctx, func(obj *v1.ConfigMap) error {
		data, err := yaml.Marshal(values)
		if err != nil {
			return err
		}

		obj.Data[moduleName] = string(data)
		return nil
	})
}

func (b Backend) updateConfigMap(ctx context.Context, transformFn func(*v1.ConfigMap) error) error {
	var err error

	obj, err := b.getConfigMap(ctx)
	if err != nil {
		return nil
	}

	isUpdate := true
	if obj == nil {
		obj = &v1.ConfigMap{}
		obj.Name = b.name
		isUpdate = false
	}

	if obj.Data == nil {
		obj.Data = make(map[string]string)
	}

	err = transformFn(obj)
	if err != nil {
		return err
	}

	if isUpdate {
		_, err = b.client.CoreV1().ConfigMaps(b.namespace).Update(ctx, obj, metav1.UpdateOptions{})
	} else {
		_, err = b.client.CoreV1().ConfigMaps(b.namespace).Create(ctx, obj, metav1.CreateOptions{})
	}
	return err
}

func (b Backend) StartInformer(ctx context.Context, eventC chan config.Event) {
	// define resyncPeriod for informer
	resyncPeriod := time.Duration(5) * time.Minute

	// define indexers for informer
	indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}

	// define tweakListOptions for informer
	tweakListOptions := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("metadata.name", b.name).String()
	}

	cmInformer := corev1.NewFilteredConfigMapInformer(b.client, b.namespace, resyncPeriod, indexers, tweakListOptions)
	_, _ = cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			b.logConfigMapEvent(ctx, obj, "add")
			err := b.handleConfigMapEvent(obj.(*v1.ConfigMap), eventC)
			if err != nil {
				b.logger.Error("Handle ConfigMap 'add' error",
					slog.String("configMapName", b.name),
					log.Err(err))
			}
		},
		UpdateFunc: func(_ interface{}, obj interface{}) {
			b.logConfigMapEvent(ctx, obj, "update")
			err := b.handleConfigMapEvent(obj.(*v1.ConfigMap), eventC)
			if err != nil {
				b.logger.Error("Handle ConfigMap 'update' error",
					slog.String("configMapName", b.name),
					log.Err(err))
			}
		},
		DeleteFunc: func(obj interface{}) {
			b.logConfigMapEvent(ctx, obj, "delete")
			_ = b.handleConfigMapEvent(nil, eventC)
		},
	})

	go func() {
		cmInformer.Run(ctx.Done())
	}()
}

func (b Backend) logConfigMapEvent(ctx context.Context, obj interface{}, eventName string) {
	if !b.isDebugEnabled(ctx) {
		return
	}

	objYaml, err := yaml.Marshal(obj)
	if err != nil {
		b.logger.Info("Dump ConfigMap error",
			slog.String("configMapName", b.name),
			slog.String("eventName", eventName),
			log.Err(err))
		return
	}
	b.logger.Info("Dump ConfigMap",
		slog.String("configMapName", b.name),
		slog.String("eventName", eventName),
		slog.String("value", string(objYaml)))
}

func (b Backend) handleConfigMapEvent(obj *v1.ConfigMap, eventC chan config.Event) error {
	// ConfigMap is deleted, reset cached config and fire event.
	if obj == nil {
		eventC <- config.Event{Key: ""}
		return nil
	}

	newConfig, err := parseConfigMapData(obj.Data)
	if err != nil {
		eventC <- config.Event{Key: "batch", Err: err}
		// Do not update caches to detect changes on next update.
		b.logger.Error("ConfigMap invalid",
			slog.String("configMapName", b.name),
			log.Err(err))
		return err
	}

	eventC <- config.Event{Key: "batch", Config: newConfig}

	return nil
}
