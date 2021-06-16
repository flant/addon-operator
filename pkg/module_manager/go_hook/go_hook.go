package go_hook

import (
	"time"

	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
)

type GoHook interface {
	Config() *HookConfig
	Run(input *HookInput) error
}

type ObjectPatcher interface {
	CreateObject(object *unstructured.Unstructured, subresource string) error
	CreateOrUpdateObject(object *unstructured.Unstructured, subresource string) error
	FilterObject(filterFunc func(*unstructured.Unstructured) (*unstructured.Unstructured, error),
		apiVersion, kind, namespace, name, subresource string) error
	JQPatchObject(jqPatch, apiVersion, kind, namespace, name, subresource string) error
	MergePatchObject(mergePatch []byte, apiVersion, kind, namespace, name, subresource string) error
	JSONPatchObject(jsonPatch []byte, apiVersion, kind, namespace, name, subresource string) error
	DeleteObject(apiVersion, kind, namespace, name, subresource string) error
	DeleteObjectInBackground(apiVersion, kind, namespace, name, subresource string) error
	DeleteObjectNonCascading(apiVersion, kind, namespace, name, subresource string) error
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
	ObjectPatcher    ObjectPatcher
	LogEntry         *logrus.Entry
}

type HookConfig struct {
	Schedule          []ScheduleConfig
	Kubernetes        []KubernetesConfig
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
}

type ScheduleConfig struct {
	Name    string
	Crontab string
}

type FilterFunc func(*unstructured.Unstructured) (FilterResult, error)

type KubernetesConfig struct {
	Name                         string
	ApiVersion                   string
	Kind                         string
	NameSelector                 *types.NameSelector
	NamespaceSelector            *types.NamespaceSelector
	LabelSelector                *v1.LabelSelector
	FieldSelector                *types.FieldSelector
	ExecuteHookOnEvents          *bool
	ExecuteHookOnSynchronization *bool
	WaitForSynchronization       *bool
	FilterFunc                   FilterFunc
}

type OrderedConfig struct {
	Order float64
}

type HookBindingContext struct {
	Type      string // type: Event Synchronization Group Schedule
	Binding   string // binding name
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
