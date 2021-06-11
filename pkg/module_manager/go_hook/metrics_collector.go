package go_hook

import (
	"github.com/flant/shell-operator/pkg/metric_storage/operation"
	"k8s.io/utils/pointer"
)

type inMemoryMetricsCollector struct {
	metrics []operation.MetricOperation
}

func NewMetricsCollector() *inMemoryMetricsCollector {
	return &inMemoryMetricsCollector{metrics: make([]operation.MetricOperation, 0)}
}

func (dms *inMemoryMetricsCollector) Add(name string, value float64) {
	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Add:    pointer.Float64Ptr(value),
		Action: "add",
	})
}

func (dms *inMemoryMetricsCollector) Set(name string, value float64) {
	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Set:    pointer.Float64Ptr(value),
		Action: "set",
	})
}

// CollectMetrics returns all collected metrics
func (dms *inMemoryMetricsCollector) CollectedMetrics() []operation.MetricOperation {
	return dms.metrics
}
