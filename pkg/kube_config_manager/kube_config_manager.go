package kube_config_manager

import (
	"context"
	"fmt"
	"os"
	"time"

	klient "github.com/flant/kube-client/client"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/flant/addon-operator/pkg/utils"
)

type KubeConfigManager interface {
	WithContext(ctx context.Context)
	WithKubeClient(client klient.Client)
	WithNamespace(namespace string)
	WithConfigMapName(configMap string)
	SetKubeGlobalValues(values utils.Values) error
	SetKubeModuleValues(moduleName string, values utils.Values) error
	Init() error
	Start()
	Stop()
	InitialConfig() *Config
	CurrentConfig() *Config
}

type kubeConfigManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	KubeClient    klient.Client
	Namespace     string
	ConfigMapName string

	initialConfig *Config
	currentConfig *Config

	GlobalValuesChecksum  string
	ModulesValuesChecksum map[string]string
}

// kubeConfigManager should implement KubeConfigManager
var _ KubeConfigManager = &kubeConfigManager{}

type ModuleConfigs map[string]utils.ModuleConfig

func (m ModuleConfigs) Names() []string {
	names := make([]string, 0)
	for _, newModuleConfig := range m {
		names = append(names, fmt.Sprintf("'%s'", newModuleConfig.ModuleName))
	}
	return names
}

type Config struct {
	Values        utils.Values
	ModuleConfigs ModuleConfigs
}

func NewConfig() *Config {
	return &Config{
		Values:        make(utils.Values),
		ModuleConfigs: make(map[string]utils.ModuleConfig),
	}
}

var (
	VerboseDebug bool
	// ConfigUpdated chan receives a new Config when global values are changed
	ConfigUpdated chan Config
	// ModuleConfigsUpdated chan receives a list of all ModuleConfig in configData. Updated items marked as IsUpdated.
	ModuleConfigsUpdated chan ModuleConfigs
)

func simpleMergeConfigMapData(data map[string]string, newData map[string]string) map[string]string {
	for k, v := range newData {
		data[k] = v
	}
	return data
}

func (kcm *kubeConfigManager) WithContext(ctx context.Context) {
	kcm.ctx, kcm.cancel = context.WithCancel(ctx)
}

func (kcm *kubeConfigManager) Stop() {
	if kcm.cancel != nil {
		kcm.cancel()
	}
}

func (kcm *kubeConfigManager) WithKubeClient(client klient.Client) {
	kcm.KubeClient = client
}

func (kcm *kubeConfigManager) saveGlobalKubeConfig(globalKubeConfig GlobalKubeConfig) error {
	err := kcm.changeOrCreateKubeConfig(func(obj *v1.ConfigMap) error {
		obj.Data = simpleMergeConfigMapData(obj.Data, globalKubeConfig.ConfigData)
		return nil
	})
	if err != nil {
		return err
	}
	// If ConfigMap is updated, save checksum for global section.
	kcm.GlobalValuesChecksum = globalKubeConfig.Checksum
	return nil
}

func (kcm *kubeConfigManager) saveModuleKubeConfig(moduleKubeConfig ModuleKubeConfig) error {
	err := kcm.changeOrCreateKubeConfig(func(obj *v1.ConfigMap) error {
		obj.Data = simpleMergeConfigMapData(obj.Data, moduleKubeConfig.ConfigData)
		return nil
	})
	if err != nil {
		return err
	}
	// TODO add a mutex for this map? Config patch from hook can run in parallel with ConfigMap editing...
	kcm.ModulesValuesChecksum[moduleKubeConfig.ModuleName] = moduleKubeConfig.Checksum
	return nil
}

func (kcm *kubeConfigManager) changeOrCreateKubeConfig(configChangeFunc func(*v1.ConfigMap) error) error {
	var err error

	obj, err := kcm.getConfigMap()
	if err != nil {
		return nil
	}

	if obj != nil {
		if obj.Data == nil {
			obj.Data = make(map[string]string)
		}

		err = configChangeFunc(obj)
		if err != nil {
			return err
		}

		_, err := kcm.KubeClient.CoreV1().ConfigMaps(kcm.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		return nil
	} else {
		obj := &v1.ConfigMap{}
		obj.Name = kcm.ConfigMapName
		obj.Data = make(map[string]string)

		err = configChangeFunc(obj)
		if err != nil {
			return err
		}

		_, err := kcm.KubeClient.CoreV1().ConfigMaps(kcm.Namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
		if err != nil {
			return err
		}

		return nil
	}
}

func (kcm *kubeConfigManager) WithNamespace(namespace string) {
	kcm.Namespace = namespace
}

func (kcm *kubeConfigManager) WithConfigMapName(configMap string) {
	kcm.ConfigMapName = configMap
}

func (kcm *kubeConfigManager) SetKubeGlobalValues(values utils.Values) error {
	globalKubeConfig, err := GetGlobalKubeConfigFromValues(values)
	if err != nil {
		return err
	}

	if globalKubeConfig != nil {
		log.Debugf("Kube config manager: set kube global values:\n%s", values.DebugString())

		err := kcm.saveGlobalKubeConfig(*globalKubeConfig)
		if err != nil {
			return err
		}
	}

	return nil
}

func (kcm *kubeConfigManager) SetKubeModuleValues(moduleName string, values utils.Values) error {
	moduleKubeConfig, err := GetModuleKubeConfigFromValues(moduleName, values)
	if err != nil {
		return err
	}

	if moduleKubeConfig != nil {
		log.Debugf("Kube config manager: set kube module values:\n%s", moduleKubeConfig.ModuleConfig.String())

		err := kcm.saveModuleKubeConfig(*moduleKubeConfig)
		if err != nil {
			return err
		}
	}

	return nil
}

func (kcm *kubeConfigManager) getConfigMap() (*v1.ConfigMap, error) {
	list, err := kcm.KubeClient.CoreV1().
		ConfigMaps(kcm.Namespace).
		List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	objExists := false
	for _, obj := range list.Items {
		if obj.ObjectMeta.Name == kcm.ConfigMapName {
			objExists = true
			break
		}
	}

	if objExists {
		obj, err := kcm.KubeClient.CoreV1().
			ConfigMaps(kcm.Namespace).
			Get(context.TODO(), kcm.ConfigMapName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		log.Debugf("KUBE_CONFIG_MANAGER: Will use ConfigMap/%s for persistent values", kcm.ConfigMapName)
		return obj, nil
	} else {
		log.Debugf("KUBE_CONFIG_MANAGER: ConfigMap/%s is not created", kcm.ConfigMapName)
		return nil, nil
	}
}

func (kcm *kubeConfigManager) InitialConfig() *Config {
	return kcm.initialConfig
}

func (kcm *kubeConfigManager) CurrentConfig() *Config {
	return kcm.currentConfig
}

func NewKubeConfigManager() KubeConfigManager {
	return &kubeConfigManager{
		initialConfig:         NewConfig(),
		currentConfig:         NewConfig(),
		ModulesValuesChecksum: map[string]string{},
	}
}

func (kcm *kubeConfigManager) initConfig() error {
	obj, err := kcm.getConfigMap()
	if err != nil {
		return err
	}

	if obj == nil {
		log.Infof("Init config from ConfigMap: cm/%s is not found", kcm.ConfigMapName)
		return nil
	}

	initialConfig := NewConfig()
	globalValuesChecksum := ""
	modulesValuesChecksum := make(map[string]string)

	globalKubeConfig, err := GetGlobalKubeConfigFromConfigData(obj.Data)
	if err != nil {
		return err
	}
	if globalKubeConfig != nil {
		initialConfig.Values = globalKubeConfig.Values
		globalValuesChecksum = globalKubeConfig.Checksum
	}

	for moduleName := range GetModulesNamesFromConfigData(obj.Data) {
		// all GetModulesNamesFromConfigData must exist
		moduleKubeConfig, err := ExtractModuleKubeConfig(moduleName, obj.Data)
		if err != nil {
			return err
		}

		initialConfig.ModuleConfigs[moduleKubeConfig.ModuleName] = moduleKubeConfig.ModuleConfig
		modulesValuesChecksum[moduleKubeConfig.ModuleName] = moduleKubeConfig.Checksum
	}

	kcm.initialConfig = initialConfig
	kcm.currentConfig = initialConfig
	kcm.GlobalValuesChecksum = globalValuesChecksum
	kcm.ModulesValuesChecksum = modulesValuesChecksum

	return nil
}

func (kcm *kubeConfigManager) Init() error {
	log.Debug("INIT: KUBE_CONFIG")

	VerboseDebug = false
	if os.Getenv("KUBE_CONFIG_MANAGER_DEBUG") != "" {
		VerboseDebug = true
	}

	ConfigUpdated = make(chan Config, 1)
	ModuleConfigsUpdated = make(chan ModuleConfigs, 1)

	err := kcm.initConfig()
	if err != nil {
		return err
	}

	return nil
}

// handleNewCm determine changes in kube config.
//
// New Config is send over ConfigUpdate channel if global section is changed.
//
// Array of actual ModuleConfig is send over ModuleConfigsUpdated channel
// if module sections are changed or deleted.
func (kcm *kubeConfigManager) handleNewCm(obj *v1.ConfigMap) error {
	globalKubeConfig, err := GetGlobalKubeConfigFromConfigData(obj.Data)
	if err != nil {
		return err
	}

	// if global values are changed or deleted then new config should be sent over ConfigUpdated channel
	isGlobalUpdated := globalKubeConfig != nil &&
		globalKubeConfig.Checksum != kcm.GlobalValuesChecksum
	isGlobalDeleted := globalKubeConfig == nil && kcm.GlobalValuesChecksum != ""

	if isGlobalUpdated || isGlobalDeleted {
		log.Infof("Kube config manager: detect changes in global section")
		newConfig := NewConfig()

		// calculate new checksum of a global section
		newGlobalValuesChecksum := ""
		if globalKubeConfig != nil {
			newConfig.Values = globalKubeConfig.Values
			newGlobalValuesChecksum = globalKubeConfig.Checksum
		}
		kcm.GlobalValuesChecksum = newGlobalValuesChecksum

		// calculate new checksums of a module sections
		newModulesValuesChecksum := make(map[string]string)
		for moduleName := range GetModulesNamesFromConfigData(obj.Data) {
			// all GetModulesNamesFromConfigData must exist
			moduleKubeConfig, err := ExtractModuleKubeConfig(moduleName, obj.Data)
			if err != nil {
				return err
			}

			newConfig.ModuleConfigs[moduleKubeConfig.ModuleName] = moduleKubeConfig.ModuleConfig
			newModulesValuesChecksum[moduleKubeConfig.ModuleName] = moduleKubeConfig.Checksum
		}
		kcm.ModulesValuesChecksum = newModulesValuesChecksum

		log.Debugf("Kube config manager: global section new values:\n%s",
			newConfig.Values.DebugString())
		for _, moduleConfig := range newConfig.ModuleConfigs {
			log.Debugf("%s", moduleConfig.String())
		}

		ConfigUpdated <- *newConfig

		kcm.currentConfig = newConfig
	} else {
		actualModulesNames := GetModulesNamesFromConfigData(obj.Data)

		moduleConfigsActual := make(ModuleConfigs)
		updatedCount := 0
		removedCount := 0

		// create ModuleConfig for each module in configData
		// IsUpdated flag set for updated configs
		for moduleName := range actualModulesNames {
			// all GetModulesNamesFromConfigData must exist
			moduleKubeConfig, err := ExtractModuleKubeConfig(moduleName, obj.Data)
			if err != nil {
				return err
			}

			if moduleKubeConfig.Checksum != kcm.ModulesValuesChecksum[moduleName] {
				kcm.ModulesValuesChecksum[moduleName] = moduleKubeConfig.Checksum
				moduleKubeConfig.ModuleConfig.IsUpdated = true
				updatedCount++
			} else {
				moduleKubeConfig.ModuleConfig.IsUpdated = false
			}
			moduleConfigsActual[moduleName] = moduleKubeConfig.ModuleConfig
		}

		// delete checksums for removed module sections
		for module := range kcm.ModulesValuesChecksum {
			if _, isActual := actualModulesNames[module]; isActual {
				continue
			}
			delete(kcm.ModulesValuesChecksum, module)
			removedCount++
		}

		if updatedCount > 0 || removedCount > 0 {
			log.Infof("KUBE_CONFIG Detect module sections changes: %d updated, %d removed", updatedCount, removedCount)
			for _, moduleConfig := range moduleConfigsActual {
				log.Debugf("%s", moduleConfig.String())
			}
			ModuleConfigsUpdated <- moduleConfigsActual
			kcm.currentConfig.ModuleConfigs = moduleConfigsActual
		}
	}

	return nil
}

func (kcm *kubeConfigManager) handleCmAdd(obj *v1.ConfigMap) error {
	if VerboseDebug {
		objYaml, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		log.Debugf("Kube config manager: informer: handle ConfigMap '%s' add:\n%s", obj.Name, objYaml)
	}

	return kcm.handleNewCm(obj)
}

func (kcm *kubeConfigManager) handleCmUpdate(_ *v1.ConfigMap, obj *v1.ConfigMap) error {
	if VerboseDebug {
		objYaml, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		log.Debugf("Kube config manager: informer: handle ConfigMap '%s' update:\n%s", obj.Name, objYaml)
	}

	return kcm.handleNewCm(obj)
}

func (kcm *kubeConfigManager) handleCmDelete(obj *v1.ConfigMap) error {
	if VerboseDebug {
		objYaml, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		log.Debugf("Kube config manager: handle ConfigMap '%s' delete:\n%s", obj.Name, objYaml)
	}

	if kcm.GlobalValuesChecksum != "" {
		kcm.GlobalValuesChecksum = ""
		kcm.ModulesValuesChecksum = make(map[string]string)

		ConfigUpdated <- Config{
			Values:        make(utils.Values),
			ModuleConfigs: make(map[string]utils.ModuleConfig),
		}
	} else {
		// Global values is already known to be empty.
		// So check each module values change separately,
		// and generate signals per-module.
		// Note: Only ModuleName field is needed in ModuleConfig.

		moduleConfigsUpdate := make(ModuleConfigs)

		updateModulesNames := make([]string, 0)
		for module := range kcm.ModulesValuesChecksum {
			updateModulesNames = append(updateModulesNames, module)
		}
		for _, module := range updateModulesNames {
			delete(kcm.ModulesValuesChecksum, module)
			moduleConfigsUpdate[module] = utils.ModuleConfig{
				ModuleName: module,
				Values:     make(utils.Values),
			}
		}

		ModuleConfigsUpdated <- moduleConfigsUpdate
	}

	return nil
}

func (kcm *kubeConfigManager) Start() {
	log.Debugf("Run kube config manager")

	// define resyncPeriod for informer
	resyncPeriod := time.Duration(5) * time.Minute

	// define indexers for informer
	indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}

	// define tweakListOptions for informer
	tweakListOptions := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("metadata.name", kcm.ConfigMapName).String()
	}

	cmInformer := corev1.NewFilteredConfigMapInformer(kcm.KubeClient, kcm.Namespace, resyncPeriod, indexers, tweakListOptions)
	cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			err := kcm.handleCmAdd(obj.(*v1.ConfigMap))
			if err != nil {
				log.Errorf("Kube config manager: cannot handle ConfigMap add: %s", err)
			}
		},
		UpdateFunc: func(prevObj interface{}, obj interface{}) {
			err := kcm.handleCmUpdate(prevObj.(*v1.ConfigMap), obj.(*v1.ConfigMap))
			if err != nil {
				log.Errorf("Kube config manager: cannot handle ConfigMap update: %s", err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			err := kcm.handleCmDelete(obj.(*v1.ConfigMap))
			if err != nil {
				log.Errorf("Kube config manager: cannot handle ConfigMap delete: %s", err)
			}
		},
	})

	cmInformer.Run(kcm.ctx.Done())
}
