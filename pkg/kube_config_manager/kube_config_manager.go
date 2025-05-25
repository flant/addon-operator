package kube_config_manager

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/kube_config_manager/backend"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
	runtimeConfig "github.com/flant/shell-operator/pkg/config"
)

// KubeConfigManager watches for changes in ConfigMap/addon-operator and provides
// methods to change its content.
// It stores values parsed from ConfigMap data. OpenAPI validation of these config values
// is not a responsibility of this component.
type KubeConfigManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *log.Logger

	// Checksums to ignore self-initiated updates.
	knownChecksums *Checksums

	// Channel to emit events.
	configEventCh chan config.KubeConfigEvent
	backend       backend.ConfigHandler

	m             sync.RWMutex
	currentConfig *config.KubeConfig
}

func NewKubeConfigManager(ctx context.Context, bk backend.ConfigHandler, runtimeConfig *runtimeConfig.Config, logger *log.Logger) *KubeConfigManager {
	cctx, cancel := context.WithCancel(ctx)
	logger = logger.With("component", "KubeConfigManager")
	logger.With("backend", fmt.Sprintf("%T", bk)).Info("Setup KubeConfigManager backend")

	// Runtime config to enable logging all events from the ConfigMap at runtime.
	if runtimeConfig != nil {
		runtimeConfig.Register(
			"log.configmap.events",
			fmt.Sprintf("Set to true to log all operations with Configuration manager/%s", reflect.TypeOf(bk)),
			"false",
			func(_ string, newValue string) error {
				val, err := strconv.ParseBool(newValue)
				if err != nil {
					return err
				}
				//nolint: revive,staticcheck // basic type is enough here
				cctx = context.WithValue(cctx, "kube-config-manager-debug", val)
				return nil
			},
			nil,
		)
	}

	return &KubeConfigManager{
		ctx:    cctx,
		cancel: cancel,

		currentConfig:  config.NewConfig(),
		knownChecksums: NewChecksums(),
		configEventCh:  make(chan config.KubeConfigEvent, 1),
		logger:         logger,
		backend:        bk,
	}
}

func (kcm *KubeConfigManager) IsModuleEnabled(moduleName string) *bool {
	kcm.m.RLock()
	defer kcm.m.RUnlock()
	moduleConfig, found := kcm.currentConfig.Modules[moduleName]
	if !found {
		return nil
	}

	return moduleConfig.IsEnabled
}

func (kcm *KubeConfigManager) Init() error {
	kcm.logger.Debug("Init: KubeConfigManager")

	// Load config and calculate checksums at start.
	err := kcm.loadConfig()
	if err != nil {
		return err
	}

	return nil
}

// SaveConfigValues updates `global` or `module` section in ConfigMap.
// It uses knownChecksums to prevent KubeConfigChanged event on self-update.
func (kcm *KubeConfigManager) SaveConfigValues(key string, values utils.Values) error {
	checksum, err := kcm.backend.SaveConfigValues(kcm.ctx, key, values)
	if err != nil {
		kcm.withLock(func() {
			kcm.knownChecksums.Remove(key, checksum)
		})

		return err
	}

	kcm.withLock(func() {
		kcm.knownChecksums.Add(key, checksum)
	})

	return nil
}

// KubeConfigEventCh return a channel that emits new KubeConfig on ConfigMap changes in global section or enabled modules.
func (kcm *KubeConfigManager) KubeConfigEventCh() chan config.KubeConfigEvent {
	return kcm.configEventCh
}

// UpdateModuleConfig updates a single module config
// Метод загружает конфигурацию вне блокировки, чтобы уменьшить время блокировки мьютекса
func (kcm *KubeConfigManager) UpdateModuleConfig(moduleName string) error {
	// Load config outside the lock to reduce contention
	newModuleConfig, err := kcm.backend.LoadConfig(kcm.ctx, moduleName)
	if err != nil {
		return err
	}

	// Используем вспомогательный метод для блокировки доступа к общему состоянию
	kcm.withLock(func() {
		if moduleConfig, found := newModuleConfig.Modules[moduleName]; found {
			kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
			kcm.currentConfig.Modules[moduleName] = moduleConfig
		}
	})

	return nil
}

// loadConfig gets config from ConfigMap before starting informer.
// Set checksums for global section and modules.
func (kcm *KubeConfigManager) loadConfig() error {
	newConfig, err := kcm.backend.LoadConfig(kcm.ctx)
	if err != nil {
		return err
	}

	// Protect access to shared state with mutex
	kcm.m.Lock()
	defer kcm.m.Unlock()

	if newConfig.Global != nil {
		kcm.knownChecksums.Set(utils.GlobalValuesKey, newConfig.Global.Checksum)
	}

	for moduleName, moduleConfig := range newConfig.Modules {
		kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
	}

	kcm.currentConfig = newConfig
	return nil
}

// isGlobalChanged returns true when changes in "global" section require firing event.
// IMPORTANT: This method MUST be called with kcm.m locked as it accesses shared state.
// Failure to do so will result in race conditions.
func (kcm *KubeConfigManager) isGlobalChanged(newConfig *config.KubeConfig) bool {
	if newConfig.Global == nil {
		// Fire event when global section is deleted: ConfigMap has no global section but global config is cached.
		// Note: no checksum checking here, "save" operations can't delete global section.
		if kcm.currentConfig.Global != nil {
			kcm.logger.Info("Global section deleted")
			return true
		}
		kcm.logger.Debug("Global section is empty")
		return false
	}

	newChecksum := newConfig.Global.Checksum
	// Global section is updated if a new checksum not equal to the saved one and not in knownChecksum.
	if kcm.knownChecksums.HasEqualChecksum(utils.GlobalValuesKey, newChecksum) {
		// Remove known checksum, do not fire event on self-update.
		kcm.knownChecksums.Remove(utils.GlobalValuesKey, newChecksum)
		kcm.logger.Debug("Global section self-update")
		return false
	}

	if kcm.currentConfig.Global == nil {
		// "global" section is added after initialization.
		kcm.logger.Info("Global section added")
		return true
	}
	// Consider "global" change when new checksum is not equal to the saved.
	if kcm.currentConfig.Global.Checksum != newChecksum {
		kcm.logger.Info("Global section updated")
		return true
	}

	return false
}

// sendEventIfNeeded sends the event on the channel if it's not nil
func (kcm *KubeConfigManager) sendEventIfNeeded(eventToSend *config.KubeConfigEvent) {
	if eventToSend != nil {
		kcm.configEventCh <- *eventToSend
	}
}

// handleConfigEvent определяет изменения в конфигурации Kubernetes. Отправляет событие KubeConfigChanged
// если что-то изменилось, или KubeConfigInvalid, если конфигурация некорректна.
// Метод оптимизирован для минимизации времени блокировки мьютекса.
func (kcm *KubeConfigManager) handleConfigEvent(obj config.Event) {
	// Обработка ошибок конфигурации без длительной блокировки
	if obj.Err != nil {
		eventToSend := kcm.handleConfigError(obj.Key, obj.Err)
		kcm.sendEventIfNeeded(eventToSend)
		return
	}

	var eventToSend *config.KubeConfigEvent

	// Обработка событий сброса конфигурации
	if obj.Key == "" {
		kcm.withLock(func() {
			eventToSend = kcm.handleResetConfig()
			if eventToSend != nil {
				// Update currentConfig with a new config instance
				kcm.currentConfig = config.NewConfig()
			}
		})
		kcm.sendEventIfNeeded(eventToSend)
		return
	}

	// Обработка изменений в глобальной секции
	if obj.Key == utils.GlobalValuesKey {
		kcm.withLock(func() {
			eventToSend = kcm.handleGlobalConfig(obj.Config)
			if eventToSend != nil {
				// Update state after successful parsing
				kcm.currentConfig.Global = obj.Config.Global
			}
		})
		kcm.sendEventIfNeeded(eventToSend)
		return
	}

	// Обработка изменений в модуле
	moduleName := obj.Key

	// Событие удаления модуля
	if obj.Op == config.EventDelete {
		kcm.withLock(func() {
			eventToSend = kcm.handleModuleDelete(moduleName)
			if eventToSend != nil {
				// Apply module deletion changes
				moduleCfg := kcm.currentConfig.Modules[moduleName]
				moduleCfg.Reset()
				moduleCfg.Checksum = moduleCfg.ModuleConfig.Checksum()
				kcm.currentConfig.Modules[moduleName] = moduleCfg
			}
		})
	} else {
		// Событие обновления модуля
		moduleCfg := obj.Config.Modules[obj.Key]
		kcm.withLock(func() {
			eventToSend = kcm.handleModuleUpdate(moduleName, moduleCfg)
			if eventToSend != nil {
				// Now we need to apply the update here since handleModuleUpdate doesn't modify currentConfig anymore
				kcm.currentConfig.Modules[moduleName] = moduleCfg
			}
		})
	}

	// Отправка события после разблокировки
	kcm.sendEventIfNeeded(eventToSend)
}

// handleBatchConfigEvent обрабатывает пакетные изменения конфигурации
// Оптимизирован для уменьшения времени блокировки мьютекса
func (kcm *KubeConfigManager) handleBatchConfigEvent(obj config.Event) {
	if obj.Err != nil {
		eventToSend := kcm.handleConfigError("batch", obj.Err)
		kcm.sendEventIfNeeded(eventToSend)
		return
	}

	if obj.Key == "" {
		// Обработка сброса конфигурации бэкенда
		kcm.withLock(func() {
			// Config backend was reset
			eventToSend := kcm.handleResetConfig()
			if eventToSend != nil {
				// Update currentConfig with a new config instance
				kcm.currentConfig = config.NewConfig()
				// Отправляем событие сразу, чтобы не нужно было освобождать блокировку
				kcm.sendEventIfNeeded(eventToSend)
			}
		})
		return
	}

	newConfig := obj.Config

	// Предварительное создание структур данных для результатов
	var (
		globalChanged            bool
		modulesChanged           []string
		modulesStateChanged      []string
		moduleMaintenanceChanged map[string]utils.Maintenance
		eventToSend              *config.KubeConfigEvent
	)

	// Подготовка данных для обработки удаленных модулей
	// Это позволяет уменьшить время блокировки мьютекса
	var currentModuleNames map[string]struct{}

	// Первая блокировка - только чтение для создания списка текущих модулей
	kcm.withRLock(func() {
		currentModuleNames = make(map[string]struct{}, len(kcm.currentConfig.Modules))
		for name := range kcm.currentConfig.Modules {
			currentModuleNames[name] = struct{}{}
		}
	})

	// Теперь обработаем обновления модулей вне блокировки для определения изменений
	modulesChanged = []string{}
	modulesStateChanged = []string{}
	moduleMaintenanceChanged = make(map[string]utils.Maintenance)

	// Блокировка для проверки и обновления состояния
	kcm.withLock(func() {
		globalChanged = kcm.isGlobalChanged(newConfig)

		// Process module updates
		for moduleName, moduleCfg := range newConfig.Modules {
			// Remove module name from current names to detect deleted sections.
			delete(currentModuleNames, moduleName)

			// Process individual module and update modules in newConfig
			if event := kcm.handleModuleUpdate(moduleName, moduleCfg); event != nil {
				modulesChanged = append(modulesChanged, event.ModuleValuesChanged...)
				modulesStateChanged = append(modulesStateChanged, event.ModuleEnabledStateChanged...)

				// Merge maintenance changes
				maps.Copy(moduleMaintenanceChanged, event.ModuleMaintenanceChanged)
			}
		}

		// Process deleted modules
		deletedModulesChanged, deletedModulesStateChanged, deletedModuleMaintenanceChanged := kcm.processBatchDeletedModules(currentModuleNames)

		if deletedModulesChanged != nil {
			modulesChanged = append(modulesChanged, deletedModulesChanged...)
		}
		if deletedModulesStateChanged != nil {
			modulesStateChanged = append(modulesStateChanged, deletedModulesStateChanged...)
		}
		if deletedModuleMaintenanceChanged != nil {
			maps.Copy(moduleMaintenanceChanged, deletedModuleMaintenanceChanged)
		}

		// Update state after successful parsing.
		kcm.currentConfig = newConfig

		// Prepare event data while holding the lock
		if globalChanged || len(modulesChanged)+len(modulesStateChanged)+len(moduleMaintenanceChanged) > 0 {
			eventToSend = &config.KubeConfigEvent{
				Type:                      config.KubeConfigChanged,
				GlobalSectionChanged:      globalChanged,
				ModuleValuesChanged:       modulesChanged,
				ModuleEnabledStateChanged: modulesStateChanged,
				ModuleMaintenanceChanged:  moduleMaintenanceChanged,
			}
		}
	})

	// Отправка события вне блокировки для предотвращения возможного дедлока
	kcm.sendEventIfNeeded(eventToSend)
}

func (kcm *KubeConfigManager) Start() {
	kcm.logger.Debug("Start kube config manager")

	go kcm.start()
}

func (kcm *KubeConfigManager) start() {
	eventC := make(chan config.Event, 100)

	kcm.backend.StartInformer(kcm.ctx, eventC)

	for {
		select {
		case event := <-eventC:
			if event.Key == "batch" {
				kcm.handleBatchConfigEvent(event)
			} else {
				kcm.handleConfigEvent(event)
			}

		case <-kcm.ctx.Done():
			kcm.logger.Debug("Stop kube config manager")
			return
		}
	}
}

func (kcm *KubeConfigManager) Stop() {
	if kcm.cancel != nil {
		kcm.cancel()
	}
}

// SafeReadConfig locks currentConfig to safely read from it in external services.
// Этот метод обеспечивает потокобезопасный доступ к конфигурации для внешних сервисов,
// используя блокировку чтения.
func (kcm *KubeConfigManager) SafeReadConfig(handler func(config *config.KubeConfig)) {
	if handler == nil {
		return
	}
	kcm.withRLock(func() {
		handler(kcm.currentConfig)
	})
}

// withRLock выполняет функцию fn под блокировкой чтения мьютекса.
// Используйте для безопасного чтения защищенного состояния.
func (kcm *KubeConfigManager) withRLock(fn func()) {
	if fn == nil {
		return
	}
	kcm.m.RLock()
	fn()
	kcm.m.RUnlock()
}

// withLock выполняет функцию fn под эксклюзивной блокировкой мьютекса.
// Используйте для безопасного изменения защищенного состояния.
func (kcm *KubeConfigManager) withLock(fn func()) {
	if fn == nil {
		return
	}
	kcm.m.Lock()
	fn()
	kcm.m.Unlock()
}
