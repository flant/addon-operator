package hooks

import (
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"
)

type hooksMetricsStorage interface {
	SendBatch([]metric_operation.MetricOperation, map[string]string) error
}

type kubeConfigManager interface {
	SaveConfigValues(moduleName string, configValuesPatch utils.Values) error
}

type metricStorage interface {
	HistogramObserve(metric string, value float64, labels map[string]string, buckets []float64)
	GaugeSet(metric string, value float64, labels map[string]string)
}

type kubeObjectPatcher interface {
	ExecuteOperations([]object_patch.Operation) error
}

type globalValuesGetter interface {
	GetValues(bool) utils.Values
	GetConfigValues(bool) utils.Values
}

type HookExecutionDependencyContainer struct {
	HookMetricsStorage hooksMetricsStorage
	KubeConfigManager  kubeConfigManager
	KubeObjectPatcher  kubeObjectPatcher
	MetricStorage      metricStorage
	GlobalValuesGetter globalValuesGetter
}
