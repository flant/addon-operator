package hooks

import (
	"context"

	sdkpkg "github.com/deckhouse/module-sdk/pkg"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/utils"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/config"
	"github.com/flant/shell-operator/pkg/hook/controller"
	metricoperation "github.com/flant/shell-operator/pkg/metric_storage/operation"
)

type hooksMetricsStorage interface {
	SendBatch([]metricoperation.MetricOperation, map[string]string) error
}

type kubeConfigManager interface {
	SaveConfigValues(moduleName string, configValuesPatch utils.Values) error
}

type metricStorage interface {
	HistogramObserve(metric string, value float64, labels map[string]string, buckets []float64)
	GaugeSet(metric string, value float64, labels map[string]string)
}

type kubeObjectPatcher interface {
	ExecuteOperations([]sdkpkg.PatchCollectorOperation) error
}

type globalValuesGetter interface {
	GetValues(bool) utils.Values
	GetConfigValues(bool) utils.Values
}

// HookExecutionDependencyContainer container for all hook execution dependencies
type HookExecutionDependencyContainer struct {
	HookMetricsStorage hooksMetricsStorage
	KubeConfigManager  kubeConfigManager
	KubeObjectPatcher  kubeObjectPatcher
	MetricStorage      metricStorage
	GlobalValuesGetter globalValuesGetter
}

type executableHook interface {
	GetName() string
	GetPath() string

	Execute(configVersion string, bContext []bindingcontext.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (result *kind.HookResult, err error)
	RateLimitWait(ctx context.Context) error

	WithHookController(ctrl *controller.HookController)
	GetHookController() *controller.HookController
	WithTmpDir(tmpDir string)

	GetKind() kind.HookKind

	BackportHookConfig(cfg *config.HookConfig)
	GetHookConfigDescription() string
}

type hookConfigLoader gohook.HookConfigLoader
