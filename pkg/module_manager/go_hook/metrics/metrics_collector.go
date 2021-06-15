package metrics

import (
	"github.com/flant/shell-operator/pkg/metric_storage/operation"
	"k8s.io/utils/pointer"
)

type inMemoryMetricsCollector struct {
	defaultGroup string

	metrics []operation.MetricOperation
}

func NewMetricsCollector(defaultGroup string) *inMemoryMetricsCollector {
	return &inMemoryMetricsCollector{defaultGroup: defaultGroup, metrics: make([]operation.MetricOperation, 0)}
}

func (dms *inMemoryMetricsCollector) Inc(name string, labels map[string]string, opts ...Option) {
	dms.Add(name, 1, labels, opts...)
}

func (dms *inMemoryMetricsCollector) Add(name string, value float64, labels map[string]string, options ...Option) {
	opts := dms.defaultMetricsOptions()

	for _, opt := range options {
		opt(opts)
	}

	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Group:  opts.group,
		Add:    pointer.Float64Ptr(value),
		Action: "add",
		Labels: labels,
	})
}

func (dms *inMemoryMetricsCollector) Set(name string, value float64, labels map[string]string, options ...Option) {
	opts := dms.defaultMetricsOptions()

	for _, opt := range options {
		opt(opts)
	}

	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Group:  opts.group,
		Set:    pointer.Float64Ptr(value),
		Action: "set",
		Labels: labels,
	})
}

func (dms *inMemoryMetricsCollector) Expire(group string) {
	if group == "" {
		group = dms.defaultGroup
	}
	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Group:  group,
		Action: "expire",
	})
}

// CollectMetrics returns all collected metrics
func (dms *inMemoryMetricsCollector) CollectedMetrics() []operation.MetricOperation {
	return dms.metrics
}

func (dms *inMemoryMetricsCollector) defaultMetricsOptions() *metricsOptions {
	return &metricsOptions{group: dms.defaultGroup}
}

type metricsOptions struct {
	group string
}

type Option func(options *metricsOptions)

// WithGroup pass group for metric
func WithGroup(group string) Option {
	return func(options *metricsOptions) {
		options.group = group
	}
}