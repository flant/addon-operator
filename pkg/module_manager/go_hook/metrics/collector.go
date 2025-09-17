package metrics

import (
	"github.com/deckhouse/deckhouse/pkg/metrics-storage/operation"
	sdkpkg "github.com/deckhouse/module-sdk/pkg"
	pointer "k8s.io/utils/ptr"
)

var _ sdkpkg.MetricsCollector = (*MemoryMetricsCollector)(nil)

type MemoryMetricsCollector struct {
	defaultGroup string

	metrics []operation.MetricOperation
}

// NewCollector creates new metrics collector
func NewCollector(defaultGroup string) *MemoryMetricsCollector {
	return &MemoryMetricsCollector{defaultGroup: defaultGroup, metrics: make([]operation.MetricOperation, 0)}
}

// Inc increments specified Counter metric
func (dms *MemoryMetricsCollector) Inc(name string, labels map[string]string, opts ...sdkpkg.MetricCollectorOption) {
	dms.Add(name, 1, labels, opts...)
}

// Add adds custom value for Counter metric
func (dms *MemoryMetricsCollector) Add(name string, value float64, labels map[string]string, opts ...sdkpkg.MetricCollectorOption) {
	options := dms.defaultMetricsOptions()

	for _, opt := range opts {
		opt.Apply(options)
	}

	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Group:  options.group,
		Action: operation.ActionCounterAdd,
		Value:  pointer.To(value),
		Labels: labels,
	})
}

// Set specifies custom value for Gauge metric
func (dms *MemoryMetricsCollector) Set(name string, value float64, labels map[string]string, opts ...sdkpkg.MetricCollectorOption) {
	options := dms.defaultMetricsOptions()

	for _, opt := range opts {
		opt.Apply(options)
	}

	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Name:   name,
		Group:  options.group,
		Action: operation.ActionGaugeAdd,
		Value:  pointer.To(value),
		Labels: labels,
	})
}

// Expire marks metric's group as expired
func (dms *MemoryMetricsCollector) Expire(group string) {
	if group == "" {
		group = dms.defaultGroup
	}
	dms.metrics = append(dms.metrics, operation.MetricOperation{
		Group:  group,
		Action: operation.ActionExpireMetrics,
	})
}

// CollectedMetrics returns all collected metrics
func (dms *MemoryMetricsCollector) CollectedMetrics() []operation.MetricOperation {
	return dms.metrics
}

func (dms *MemoryMetricsCollector) defaultMetricsOptions() *metricsOptions {
	return &metricsOptions{group: dms.defaultGroup}
}

type metricsOptions struct {
	group string
}

func (o *metricsOptions) WithGroup(group string) {
	o.group = group
}

type Option func(o sdkpkg.MetricCollectorOptionApplier)

func (opt Option) Apply(o sdkpkg.MetricCollectorOptionApplier) {
	opt(o)
}

func WithGroup(group string) Option {
	return func(o sdkpkg.MetricCollectorOptionApplier) {
		o.WithGroup(group)
	}
}
