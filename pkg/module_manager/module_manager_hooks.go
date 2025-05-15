package module_manager

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	"github.com/flant/addon-operator/pkg/utils"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/hook/controller"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

type globalValues struct {
	globalValues   utils.Values
	enabledModules map[string]struct{}
	configSchema   []byte // config-values.yaml
	valuesSchema   []byte // values.yaml
}

func (mm *ModuleManager) loadGlobalValues() (*globalValues, error) {
	resultGlobalValues := utils.Values{}
	enabledModules := make(map[string]struct{})

	dirs := utils.SplitToPaths(mm.ModulesDir)
	for _, dir := range dirs {
		commonStaticValues, err := utils.LoadValuesFileFromDir(dir, app.StrictModeEnabled)
		if err != nil {
			return nil, err
		}

		for key, value := range commonStaticValues {
			if key == utils.GlobalValuesKey {
				section := commonStaticValues.GetKeySection(utils.GlobalValuesKey)
				resultGlobalValues = utils.MergeValues(resultGlobalValues, section)
				continue
			}

			if strings.HasSuffix(key, utils.EnabledSuffix) {
				enabled := false

				switch v := value.(type) {
				case bool:
					enabled = v
				case string:
					enabled, _ = strconv.ParseBool(v)
				default:
					return nil, fmt.Errorf("unknown type for Enabled flag: %s: %T", key, value)
				}

				if enabled {
					moduleName := utils.ModuleNameFromValuesKey(strings.TrimSuffix(key, utils.EnabledSuffix))
					enabledModules[moduleName] = struct{}{}
				}
			}
		}
	}

	cb, vb, err := utils.ReadOpenAPIFiles(filepath.Join(mm.GlobalHooksDir, "openapi"))
	if err != nil {
		return nil, err
	}

	gv := &globalValues{
		globalValues:   resultGlobalValues,
		enabledModules: enabledModules,
		configSchema:   cb,
		valuesSchema:   vb,
	}

	return gv, nil
}

func (mm *ModuleManager) registerGlobalModule(globalValues utils.Values, configBytes, valuesBytes []byte) error {
	// load and registry global hooks
	dep := hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
	}

	gm, err := modules.NewGlobalModule(mm.GlobalHooksDir, globalValues, &dep, configBytes, valuesBytes, shapp.DebugKeepTmpFiles, modules.WithLogger(mm.logger.Named("global-module")))
	if err != nil {
		return fmt.Errorf("new global module: %w", err)
	}

	mm.global = gm
	log.Info(gm.GetSchemaStorage().GlobalSchemasDescription())

	// applies a scheduler extender to follow which modules get enabled/disabled by dynamic patches
	dynamicExtender := dynamic_extender.NewExtender()
	if err := mm.moduleScheduler.AddExtender(dynamicExtender); err != nil {
		return err
	}
	// catch dynamin Enabled patches from global hooks
	go mm.runDynamicEnabledLoop(dynamicExtender)

	return mm.registerGlobalHooks(gm)
}

func (mm *ModuleManager) registerGlobalHooks(gm *modules.GlobalModule) error {
	log.Debug("Search and register global hooks")

	hks, err := gm.RegisterHooks()
	if err != nil {
		return err
	}

	for _, hk := range hks {
		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(hk.GetHookConfig().OnKubernetesEvents, mm.dependencies.KubeEventsManager, mm.logger)
		hookCtrl.InitScheduleBindings(hk.GetHookConfig().Schedules, mm.dependencies.ScheduleManager)

		hk.WithHookController(hookCtrl)
		hk.WithTmpDir(mm.TempDir)

		mm.dependencies.MetricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(hk.GetHookConfig().BindingsCount()),
			map[string]string{
				"hook":   hk.GetName(),
				"module": "", // empty "module" label for label set consistency with module hooks
			})
	}

	return nil
}

func (mm *ModuleManager) RegisterModuleHooks(ml *modules.BasicModule, logLabels map[string]string) error {
	logEntry := utils.EnrichLoggerWithLabels(mm.logger, logLabels).With(slog.String("module", ml.Name))

	hks, err := ml.RegisterHooks(logEntry)
	if err != nil {
		return fmt.Errorf("register hooks %w", err)
	}

	for _, hk := range hks {
		hookCtrl := controller.NewHookController()
		hookCtrl.InitKubernetesBindings(hk.GetHookConfig().OnKubernetesEvents, mm.dependencies.KubeEventsManager, mm.logger)
		hookCtrl.InitScheduleBindings(hk.GetHookConfig().Schedules, mm.dependencies.ScheduleManager)

		hk.WithHookController(hookCtrl)
		hk.WithTmpDir(mm.TempDir)

		mm.dependencies.MetricStorage.GaugeSet(
			"{PREFIX}binding_count",
			float64(hk.GetHookConfig().BindingsCount()),
			map[string]string{
				"module": ml.Name,
				"hook":   hk.GetName(),
			})
	}

	// set controllersReady to true
	ml.SetHooksControllersReady()

	return nil
}

// RunOnStartup is a phase of module lifecycle that runs onStartup hooks.
// It is a handler of task MODULE_RUN
// Run is a phase of module lifecycle that runs onStartup and beforeHelm hooks, helm upgrade --install command and afterHelm hook.
// It is a handler of task MODULE_RUN
func (mm *ModuleManager) RunModuleHooks(ctx context.Context, m *modules.BasicModule, bt sh_op_types.BindingType, logLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": m.Name,
		"queue":  "main",
	})

	m.SetStateEnabled(true)

	// Hooks can delete release resources, so stop resources monitor before run hooks.
	// m.moduleManager.HelmResourcesManager.PauseMonitor(m.Name)

	return m.RunHooksByBinding(ctx, bt, logLabels)
}
