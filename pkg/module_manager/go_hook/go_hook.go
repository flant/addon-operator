package go_hook

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/tidwall/gjson"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/config"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
)

type GoHook interface {
	Config() *HookConfig
	Run(input *HookInput) error
}

type HookConfigLoader interface {
	LoadAndValidate(cfg *config.HookConfig, moduleKind string) error
	LoadOnStartup() *float64
	LoadBeforeAll() *float64
	LoadAfterAll() *float64
	LoadAfterDeleteHelm() *float64
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
	Name           string
	Path           string
	Global         bool
	EmbeddedModule bool
	ModuleName     string
}

type FilterResult interface{}

type Snapshots map[string][]FilterResult

type Logger interface {
	Debug(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
	// Deprecated: use Debug instead
	Debugf(format string, args ...any)
	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
	// Deprecated: use Error instead
	Errorf(format string, args ...any)
	Fatal(msg string, args ...any)
	// Deprecated: use Fatal instead
	Fatalf(format string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	// Deprecated: use Info instead
	Infof(format string, args ...any)
	Log(ctx context.Context, level slog.Level, msg string, args ...any)
	LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr)
	// Deprecated: use Log instead
	Logf(ctx context.Context, level log.Level, format string, args ...any)
	Warn(msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	// Deprecated: use Warn instead
	Warnf(format string, args ...any)

	Enabled(ctx context.Context, level slog.Level) bool
	With(args ...any) *log.Logger
	WithGroup(name string) *log.Logger
	Named(name string) *log.Logger
	SetLevel(level log.Level)
	SetOutput(w io.Writer)
	GetLevel() log.Level
	Handler() slog.Handler
}

type PatchCollector interface {
	Create(object interface{}, options ...objectpatch.CreateOption)
	Delete(apiVersion string, kind string, namespace string, name string, options ...objectpatch.DeleteOption)
	Filter(filterFunc func(*unstructured.Unstructured) (*unstructured.Unstructured, error), apiVersion string, kind string, namespace string, name string, options ...objectpatch.FilterOption)
	JSONPatch(jsonPatch interface{}, apiVersion string, kind string, namespace string, name string, options ...objectpatch.PatchOption)
	MergePatch(mergePatch interface{}, apiVersion string, kind string, namespace string, name string, options ...objectpatch.PatchOption)
	Operations() []objectpatch.Operation
}

type PatchableValuesCollector interface {
	ArrayCount(path string) (int, error)
	Exists(path string) bool
	Get(path string) gjson.Result
	GetOk(path string) (gjson.Result, bool)
	GetPatches() []*utils.ValuesPatchOperation
	GetRaw(path string) interface{}
	Remove(path string)
	Set(path string, value interface{})
}

type HookInput struct {
	Snapshots        Snapshots
	Values           PatchableValuesCollector
	ConfigValues     PatchableValuesCollector
	MetricsCollector MetricsCollector
	PatchCollector   PatchCollector
	Logger           Logger
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
	Logger            *log.Logger
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
