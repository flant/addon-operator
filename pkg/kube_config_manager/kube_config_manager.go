package kube_config_manager

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	klient "github.com/flant/kube-client/client"
	"github.com/flant/shell-operator/pkg/config"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/flant/addon-operator/pkg/utils"
)

// KubeConfigManager watches for changes in ConfigMap/addon-operator and provides
// methods to change its content.
// It stores values parsed from ConfigMap data. OpenAPI validation of these config values
// is not a responsibility of this component.
type KubeConfigManager interface {
	WithContext(ctx context.Context)
	WithKubeClient(client klient.Client)
	WithNamespace(namespace string)
	WithConfigMapName(configMap string)
	WithRuntimeConfig(config *config.Config)
	SaveGlobalConfigValues(values utils.Values) error
	SaveModuleConfigValues(moduleName string, values utils.Values) error
	Init() error
	Start()
	Stop()
	KubeConfigEventCh() chan KubeConfigEvent
	SafeReadConfig(handler func(config *KubeConfig))
}

type kubeConfigManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	KubeClient    klient.Client
	Namespace     string
	ConfigMapName string

	m             sync.Mutex
	currentConfig *KubeConfig

	// Checksums to ignore self-initiated updates.
	knownChecksums *Checksums

	// Channel to emit events.
	configEventCh chan KubeConfigEvent

	// Runtime config to enable logging all events from the ConfigMap at runtime.
	runtimeConfig      *config.Config
	logConfigMapEvents bool
	logEntry           *log.Entry
}

// kubeConfigManager should implement KubeConfigManager
var _ KubeConfigManager = &kubeConfigManager{}

func NewKubeConfigManager() KubeConfigManager {
	return &kubeConfigManager{
		currentConfig:  NewConfig(),
		knownChecksums: NewChecksums(),
		configEventCh:  make(chan KubeConfigEvent, 1),
		logEntry:       log.WithField("component", "KubeConfigManager"),
	}
}

func (kcm *kubeConfigManager) WithContext(ctx context.Context) {
	kcm.ctx, kcm.cancel = context.WithCancel(ctx)
}

func (kcm *kubeConfigManager) WithKubeClient(client klient.Client) {
	kcm.KubeClient = client
}

func (kcm *kubeConfigManager) WithNamespace(namespace string) {
	kcm.Namespace = namespace
}

func (kcm *kubeConfigManager) WithConfigMapName(configMap string) {
	kcm.ConfigMapName = configMap
}

func (kcm *kubeConfigManager) WithRuntimeConfig(config *config.Config) {
	kcm.runtimeConfig = config
}

func (kcm *kubeConfigManager) SaveGlobalConfigValues(values utils.Values) error {
	globalKubeConfig, err := GetGlobalKubeConfigFromValues(values)
	if err != nil {
		return err
	}
	if globalKubeConfig == nil {
		return nil
	}

	if kcm.logConfigMapEvents {
		kcm.logEntry.Infof("Save global values to ConfigMap/%s:\n%s", kcm.ConfigMapName, values.DebugString())
	} else {
		kcm.logEntry.Infof("Save global values to ConfigMap/%s", kcm.ConfigMapName)
	}

	// Put checksum to known to ignore self-update.
	kcm.withLock(func() {
		kcm.knownChecksums.Add(utils.GlobalValuesKey, globalKubeConfig.Checksum)
	})

	err = ConfigMapMergeValues(kcm.KubeClient, kcm.Namespace, kcm.ConfigMapName, globalKubeConfig.Values)
	if err != nil {
		// Remove known checksum on error.
		kcm.withLock(func() {
			kcm.knownChecksums.Remove(utils.GlobalValuesKey, globalKubeConfig.Checksum)
		})
		return err
	}

	return nil
}

// SaveModuleConfigValues updates module section in ConfigMap.
// It uses knownChecksums to prevent KubeConfigChanged event on self-update.
func (kcm *kubeConfigManager) SaveModuleConfigValues(moduleName string, values utils.Values) error {
	moduleKubeConfig, err := GetModuleKubeConfigFromValues(moduleName, values)
	if err != nil {
		return err
	}
	if moduleKubeConfig == nil {
		return nil
	}

	if kcm.logConfigMapEvents {
		kcm.logEntry.Infof("Save module '%s' values to ConfigMap/%s:\n%s", moduleName, kcm.ConfigMapName, values.DebugString())
	} else {
		kcm.logEntry.Infof("Save module '%s' values to ConfigMap/%s", moduleName, kcm.ConfigMapName)
	}

	// Put checksum to known to ignore self-update.
	kcm.withLock(func() {
		kcm.knownChecksums.Add(moduleName, moduleKubeConfig.Checksum)
	})

	err = ConfigMapMergeValues(kcm.KubeClient, kcm.Namespace, kcm.ConfigMapName, moduleKubeConfig.Values)
	if err != nil {
		kcm.withLock(func() {
			kcm.knownChecksums.Remove(moduleName, moduleKubeConfig.Checksum)
		})
		return err
	}

	return nil
}

// KubeConfigEventCh return a channel that emits new KubeConfig on ConfigMap changes in global section or enabled modules.
func (kcm *kubeConfigManager) KubeConfigEventCh() chan KubeConfigEvent {
	return kcm.configEventCh
}

// loadConfig gets config from ConfigMap before starting informer.
// Set checksums for global section and modules.
func (kcm *kubeConfigManager) loadConfig() error {
	obj, err := ConfigMapGet(kcm.KubeClient, kcm.Namespace, kcm.ConfigMapName)
	if err != nil {
		return err
	}

	if obj == nil {
		kcm.logEntry.Infof("Initial config from ConfigMap/%s: resource is not found", kcm.ConfigMapName)
		return nil
	}

	newConfig, err := ParseConfigMapData(obj.Data)
	if err != nil {
		return err
	}

	kcm.currentConfig = newConfig
	return nil
}

func (kcm *kubeConfigManager) Init() error {
	kcm.logEntry.Debug("INIT: KUBE_CONFIG")

	if kcm.runtimeConfig != nil {
		kcm.runtimeConfig.Register(
			"log.configmap.events",
			fmt.Sprintf("Set to true to log all operations with ConfigMap/%s", kcm.ConfigMapName),
			"false",
			func(oldValue string, newValue string) error {
				val, err := strconv.ParseBool(newValue)
				if err != nil {
					return err
				}
				kcm.logConfigMapEvents = val
				return nil
			},
			nil,
		)
	}

	// Load config and calculate checksums at start. No locking required.
	err := kcm.loadConfig()
	if err != nil {
		return err
	}

	return nil
}

// currentModuleNames gather modules names from the checksums map and from the currentConfig struct.
func (kcm *kubeConfigManager) currentModuleNames() map[string]struct{} {
	names := make(map[string]struct{})
	for name := range kcm.currentConfig.Modules {
		names[name] = struct{}{}
	}
	return names
}

// isGlobalChanged returns true when changes in "global" section requires firing event.
func (kcm *kubeConfigManager) isGlobalChanged(newConfig *KubeConfig) bool {
	if newConfig.Global == nil {
		// Fire event when global section is deleted: ConfigMap has no global section but global config is cached.
		// Note: no checksum checking here, "save" operations can't delete global section.
		if kcm.currentConfig.Global != nil {
			kcm.logEntry.Infof("Global section deleted")
			return true
		}
		kcm.logEntry.Debugf("Global section is empty")
		return false
	}

	newChecksum := newConfig.Global.Checksum
	// Global section is updated if a new checksum not equal to the saved one and not in knownChecksum.
	if kcm.knownChecksums.HasEqualChecksum(utils.GlobalValuesKey, newChecksum) {
		// Remove known checksum, do not fire event on self-update.
		kcm.knownChecksums.Remove(utils.GlobalValuesKey, newChecksum)
		kcm.logEntry.Debugf("Global section self-update")
		return false
	}

	if kcm.currentConfig.Global == nil {
		// "global" section is added after initialization.
		kcm.logEntry.Infof("Global section added")
		return true
	}
	// Consider "global" change when new checksum is not equal to the saved.
	if kcm.currentConfig.Global.Checksum != newChecksum {
		kcm.logEntry.Infof("Global section updated")
		return true
	}

	return false
}

// handleNewCm determine changes in kube config. It sends KubeConfigChanged event if something
// changed or KubeConfigInvalid event if ConfigMap is incorrect.
func (kcm *kubeConfigManager) handleCmEvent(obj *v1.ConfigMap) error {
	// ConfigMap is deleted, reset cached config and fire event.
	if obj == nil {
		kcm.m.Lock()
		kcm.currentConfig = NewConfig()
		kcm.m.Unlock()
		kcm.configEventCh <- KubeConfigChanged
		return nil
	}

	newConfig, err := ParseConfigMapData(obj.Data)
	if err != nil {
		// Do not update caches to detect changes on next update.
		kcm.configEventCh <- KubeConfigInvalid
		kcm.logEntry.Errorf("ConfigMap/%s invalid: %v", kcm.ConfigMapName, err)
		return err
	}

	// Lock to read known checksums and update config.
	kcm.m.Lock()

	globalChanged := kcm.isGlobalChanged(newConfig)

	// Parse values in module sections, create new ModuleConfigs and checksums map.
	currentModuleNames := kcm.currentModuleNames()
	modulesChanged := false

	for moduleName, moduleCfg := range newConfig.Modules {
		// Remove module name from current names to detect deleted sections.
		delete(currentModuleNames, moduleName)

		// Module section is changed if new checksum not equal to saved one and not in known checksums.
		if kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum) {
			// Remove known checksum, do not fire event on self-update.
			kcm.knownChecksums.Remove(moduleName, moduleCfg.Checksum)
		} else {
			if currModuleCfg, has := kcm.currentConfig.Modules[moduleName]; has {
				if currModuleCfg.Checksum != moduleCfg.Checksum {
					modulesChanged = true
					kcm.logEntry.Infof("Module section '%s' changed. Enabled flag transition: %s--%s",
						moduleName,
						kcm.currentConfig.Modules[moduleName].GetEnabled(),
						moduleCfg.GetEnabled(),
					)
				}
			} else {
				modulesChanged = true
				kcm.logEntry.Infof("Module section '%s' added. Enabled flag: %s", moduleName, moduleCfg.GetEnabled())
			}
		}
	}

	// currentModuleNames now contains deleted module sections.
	if len(currentModuleNames) > 0 {
		modulesChanged = true
		kcm.logEntry.Infof("Module sections deleted: %+v", currentModuleNames)
	}

	// Update state after successful parsing.
	kcm.currentConfig = newConfig
	kcm.m.Unlock()

	// Fire event if ConfigMap has changes.
	if globalChanged || modulesChanged {
		kcm.configEventCh <- KubeConfigChanged
	}

	return nil
}

func (kcm *kubeConfigManager) Start() {
	kcm.logEntry.Debugf("Start kube config manager")

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
			kcm.logConfigMapEvent(obj, "add")
			err := kcm.handleCmEvent(obj.(*v1.ConfigMap))
			if err != nil {
				kcm.logEntry.Errorf("Handle ConfigMap/%s 'add' error: %s", kcm.ConfigMapName, err)
			}
		},
		UpdateFunc: func(prevObj interface{}, obj interface{}) {
			kcm.logConfigMapEvent(obj, "update")
			err := kcm.handleCmEvent(obj.(*v1.ConfigMap))
			if err != nil {
				kcm.logEntry.Errorf("Handle ConfigMap/%s 'update' error: %s", kcm.ConfigMapName, err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			kcm.logConfigMapEvent(obj, "delete")
			_ = kcm.handleCmEvent(nil)
		},
	})

	go func() {
		cmInformer.Run(kcm.ctx.Done())
	}()
}

func (kcm *kubeConfigManager) Stop() {
	if kcm.cancel != nil {
		kcm.cancel()
	}
}

func (kcm *kubeConfigManager) logConfigMapEvent(obj interface{}, eventName string) {
	if !kcm.logConfigMapEvents {
		return
	}

	objYaml, err := yaml.Marshal(obj)
	if err != nil {
		kcm.logEntry.Infof("Dump ConfigMap/%s '%s' error: %s", kcm.ConfigMapName, eventName, err)
		return
	}
	kcm.logEntry.Infof("Dump ConfigMap/%s '%s':\n%s", kcm.ConfigMapName, eventName, objYaml)
}

// SafeReadConfig locks currentConfig to safely read from it in external services.
func (kcm *kubeConfigManager) SafeReadConfig(handler func(config *KubeConfig)) {
	if handler == nil {
		return
	}
	kcm.withLock(func() {
		handler(kcm.currentConfig)
	})
}

func (kcm *kubeConfigManager) withLock(fn func()) {
	if fn == nil {
		return
	}
	kcm.m.Lock()
	fn()
	kcm.m.Unlock()
}
