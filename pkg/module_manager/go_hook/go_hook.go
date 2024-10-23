package go_hook

import (
	"time"

	"github.com/flant/shell-operator/pkg/unilogger"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
)

type GoHook interface {
	Config() *HookConfig
	Run(input *HookInput) error
}

// MetricsCollector collects metric's records for exporting them as a batch
type MetricsCollector interface {
	// Inc increments the specified Counter metric
	Inc(name string, labels map[string]string, opts ...metrics.Option)
	// Add adds custom value for the specified Counter metric
	Add(name string, value float64, labels map[string]string, opts ...metrics.Option)
	// Set specifies the custom value for the Gauge metric
	Set(name string, value float64, labels map[string]string, opts ...metrics.Option)
	// Expire marks metric's group as expired
	Expire(group string)
}

type HookMetadata struct {
	Name       string
	Path       string
	Global     bool
	Module     bool
	ModuleName string
}

type FilterResult interface{}

type Snapshots map[string][]FilterResult

type HookInput struct {
	Snapshots        Snapshots
	Values           *PatchableValues
	ConfigValues     *PatchableValues
	MetricsCollector MetricsCollector
	PatchCollector   *object_patch.PatchCollector
	LogEntry         *unilogger.Logger
	BindingActions   *[]BindingAction
}

type BindingAction struct {
	Name       string // binding name
	Action     string // Disable / UpdateKind
	Kind       string
	ApiVersion string
}

type HookConfig struct {
	Schedule   []ScheduleConfig
	Kubernetes []KubernetesConfig
	// OnStartup runs hook on module/global startup
	// Attention! During the startup you don't have snapshots available
	// use native KubeClient to fetch resources
	OnStartup         *OrderedConfig
	OnBeforeHelm      *OrderedConfig
	OnAfterHelm       *OrderedConfig
	OnAfterDeleteHelm *OrderedConfig
	OnBeforeAll       *OrderedConfig
	OnAfterAll        *OrderedConfig
	AllowFailure      bool
	Queue             string
	Settings          *HookConfigSettings
}

type HookConfigSettings struct {
	ExecutionMinInterval time.Duration
	ExecutionBurst       int
	// EnableSchedulesOnStartup
	// set to true, if you need to run 'Schedule' hooks without waiting addon-operator readiness
	EnableSchedulesOnStartup bool
}

type ScheduleConfig struct {
	Name string
	// Crontab is a schedule config in crontab format. (5 or 6 fields)
	Crontab string
}

type FilterFunc func(*unstructured.Unstructured) (FilterResult, error)

type KubernetesConfig struct {
	// Name is a key in snapshots map.
	Name string
	// ApiVersion of objects. "v1" is used if not set.
	ApiVersion string
	// Kind of objects.
	Kind string
	// NameSelector used to subscribe on object by its name.
	NameSelector *types.NameSelector
	// NamespaceSelector used to subscribe on objects in namespaces.
	NamespaceSelector *types.NamespaceSelector
	// LabelSelector used to subscribe on objects by matching their labels.
	LabelSelector *v1.LabelSelector
	// FieldSelector used to subscribe on objects by matching specific fields (the list of fields is narrow, see shell-operator documentation).
	FieldSelector *types.FieldSelector
	// ExecuteHookOnEvents is true by default. Set to false if only snapshot update is needed.
	ExecuteHookOnEvents *bool
	// ExecuteHookOnSynchronization is true by default. Set to false if only snapshot update is needed.
	ExecuteHookOnSynchronization *bool
	// WaitForSynchronization is true by default. Set to false if beforeHelm is not required this snapshot on start.
	WaitForSynchronization *bool
	// FilterFunc used to filter object content for snapshot. Addon-operator use checksum of this filtered result to ignore irrelevant events.
	FilterFunc FilterFunc
}

type OrderedConfig struct {
	Order float64
}

type HookBindingContext struct {
	// Type of binding context: [Event, Synchronization, Group, Schedule]
	Type string
	// Binding is a related binding name.
	Binding string
	// Snapshots contain all objects for all bindings.
	Snapshots map[string][]types.ObjectAndFilterResult
}

// Bool returns a pointer to a bool.
func Bool(b bool) *bool {
	return &b
}

// BoolDeref dereferences the bool ptr and returns it if not nil, or else
// returns def.
func BoolDeref(ptr *bool, def bool) bool {
	if ptr != nil {
		return *ptr
	}
	return def
}
