package sdk

import (
	"fmt"
	"regexp"
	"runtime"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	hook_types "github.com/flant/addon-operator/pkg/hook/types"
	binding_context "github.com/flant/shell-operator/pkg/hook/binding_context"
	sh_hook_types "github.com/flant/shell-operator/pkg/hook/types"
	kem_types "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"

	"github.com/flant/addon-operator/pkg/utils"
)

type HookMetadata struct {
	Name       string
	Path       string
	Global     bool
	Module     bool
	ModuleName string
}

type HookInput struct {
	BindingContexts []binding_context.BindingContext
	Values          utils.Values
	ConfigValues    utils.Values
	LogLabels       map[string]string
	Envs            map[string]string
}

type BindingInput struct {
	BindingContext binding_context.BindingContext
	Values         utils.Values
	ConfigValues   utils.Values
	LogLabels      map[string]string
	LogEntry       *log.Entry
	Envs           map[string]string
}

type BindingOutput struct {
	ConfigValuesPatches *utils.ValuesPatch
	MemoryValuesPatches *utils.ValuesPatch
	Metrics             []metric_operation.MetricOperation
	Error               error
}

type HookOutput struct {
	ConfigValuesPatches *utils.ValuesPatch
	MemoryValuesPatches *utils.ValuesPatch
	Metrics             []metric_operation.MetricOperation
	Error               error
}

type BindingHandler func(input *BindingInput) (*BindingOutput, error)

type HookConfig struct {
	YamlConfig string // define bindings with YAML as in shell hooks.

	Schedule          []ScheduleConfig
	Kubernetes        []KubernetesConfig
	OnStartup         *OrderedConfig
	OnBeforeHelm      *OrderedConfig
	OnAfterHelm       *OrderedConfig
	OnAfterDeleteHelm *OrderedConfig
	OnBeforeAll       *OrderedConfig
	OnAfterAll        *OrderedConfig
	MainHandler       BindingHandler
	GroupHandlers     map[string]BindingHandler
}

type ScheduleConfig struct {
	Name                 string
	Crontab              string
	AllowFailure         bool
	IncludeSnapshotsFrom []string
	Queue                string
	Group                string
	Handler              BindingHandler
}

type KubernetesConfig struct {
	Name                         string
	ApiVersion                   string
	Kind                         string
	NameSelector                 *kem_types.NameSelector
	NamespaceSelector            *kem_types.NamespaceSelector
	LabelSelector                *metav1.LabelSelector
	FieldSelector                *kem_types.FieldSelector
	JqFilter                     string
	IncludeSnapshotsFrom         []string
	Queue                        string
	Group                        string
	ExecuteHookOnEvents          []kem_types.WatchEventType
	ExecuteHookOnSynchronization bool
	WaitForSynchronization       bool
	KeepFullObjectsInMemory      bool
	AllowFailure                 bool
	Handler                      BindingHandler
	FilterFunc                   func(obj *unstructured.Unstructured) (string, error)
}

type OrderedConfig struct {
	Order   float64
	Handler BindingHandler
}

type JqFilterHelper struct {
	Name            string
	JqFilterFn      func(obj *unstructured.Unstructured) (result string, err error)
	ResultConverter func(string, interface{}) error
}

type HookBindingContext struct {
	Type       string // type: Event Synchronization Group Schedule
	Binding    string // binding name
	Snapshots  map[string][]kem_types.ObjectAndFilterResult
	WatchEvent string // Added/Modified/Deleted
	Objects    []kem_types.ObjectAndFilterResult
	Object     kem_types.ObjectAndFilterResult
}

type Handlers struct {
	Main              func()
	Group             map[string]func()
	Kubernetes        map[string]func()
	Schedule          map[string]func()
	OnStartup         func()
	OnBeforeAll       func()
	OnAfterAll        func()
	OnBeforeHelm      func()
	OnAfterHelm       func()
	OnAfterDeleteHelm func()
}

type GoHook interface {
	Metadata() HookMetadata
	Config() (config *HookConfig)
	Run(input *HookInput) (output *HookOutput, err error)
}

// Register is a method to define go hooks.
// return value is for trick with
//   var _ =
var Register = func(_ GoHook) bool { return false }

type HookLoader interface {
	Load()
}

type OnAfterHookBinding struct {
	Order   int
	Handler func(bc *binding_context.BindingContext) (*HookOutput, error)
}

func (b *OnAfterHookBinding) Handle(bc *binding_context.BindingContext) (*HookOutput, error) {
	if b.Handler != nil {
		return b.Handler(bc)
	}
	return nil, nil
}

type CommonGoHook struct {
	HookConfig   *HookConfig
	HookMetadata *HookMetadata
}

// /path/.../global-hooks/a/b/c/hook-name.go
// $1 - hook path
// $3 - hook name
var globalRe = regexp.MustCompile(`/global-hooks/(([^/]+/)*([^/]+))$`)

// /path/.../modules/module-name/hooks/a/b/c/hook-name.go
// $1 - hook path
// $2 - module name
// $4 - hook name
var moduleRe = regexp.MustCompile(`/modules/(([^/]+)/hooks/([^/]+/)*([^/]+))$`)

var moduleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

// CommonMetadataFromRuntime extracts hook name and path from source filename.
// This method should be called from Metadata() implementation in each hook.
func (c *CommonGoHook) CommonMetadataFromRuntime() HookMetadata {
	if c.HookMetadata != nil {
		return *c.HookMetadata
	}
	m := &HookMetadata{
		Name:       "",
		Path:       "",
		Global:     false,
		Module:     false,
		ModuleName: "",
	}

	_, f, _, _ := runtime.Caller(1)

	matches := globalRe.FindStringSubmatch(f)
	if matches != nil {
		m.Global = true
		m.Name = matches[3]
		m.Path = matches[1]
	} else {
		matches = moduleRe.FindStringSubmatch(f)
		if matches != nil {
			m.Module = true
			m.Name = matches[4]
			m.Path = matches[1]
			modNameMatches := moduleNameRe.FindStringSubmatch(matches[2])
			if modNameMatches != nil {
				m.ModuleName = modNameMatches[1]
			}
		}
	}

	c.HookMetadata = m
	return *c.HookMetadata
}

// return it from Config() in child structs
func (c *CommonGoHook) Config(cfg *HookConfig) *HookConfig {
	if c.HookConfig != nil {
		return c.HookConfig
	}
	c.HookConfig = cfg
	return c.HookConfig
}

// Run executes a handler like in BindingContext.MapV1 or in framework/shell.
func (c *CommonGoHook) Run(input *HookInput) (*HookOutput, error) {
	logEntry := log.WithFields(utils.LabelsToLogFields(input.LogLabels)).
		WithField("output", "golang")

	out := &HookOutput{
		ConfigValuesPatches: utils.NewValuesPatch(),
		MemoryValuesPatches: utils.NewValuesPatch(),
		Metrics:             make([]metric_operation.MetricOperation, 0),
	}

	for _, bc := range input.BindingContexts {
		bindingInput := &BindingInput{
			BindingContext: bc,
			Values:         input.Values,
			ConfigValues:   input.ConfigValues,
			LogEntry:       logEntry,
			LogLabels:      input.LogLabels,
		}
		handler := func() BindingHandler {
			if bc.Metadata.Group != "" {
				h := c.HookConfig.GroupHandlers[bc.Metadata.Group]
				if h != nil {
					return h
				}
				return c.HookConfig.MainHandler
			}

			switch bc.Metadata.BindingType {
			case sh_hook_types.OnStartup:
				return c.HookConfig.OnStartup.Handler
			case hook_types.BeforeAll:
				return c.HookConfig.OnBeforeAll.Handler
			case hook_types.AfterAll:
				return c.HookConfig.OnAfterAll.Handler
			case hook_types.BeforeHelm:
				return c.HookConfig.OnBeforeHelm.Handler
			case hook_types.AfterHelm:
				return c.HookConfig.OnAfterHelm.Handler
			case hook_types.AfterDeleteHelm:
				return c.HookConfig.OnAfterDeleteHelm.Handler
			case sh_hook_types.Schedule:
				for _, sc := range c.HookConfig.Schedule {
					if sc.Name == bc.Binding {
						return sc.Handler
					}
				}
			case sh_hook_types.OnKubernetesEvent:
				// TODO split to Synchronization and Event handlers?
				for _, sc := range c.HookConfig.Kubernetes {
					if sc.Name == bc.Binding {
						return sc.Handler
					}
				}
			default:
				return c.HookConfig.MainHandler
			}

			return nil
		}()

		if handler == nil {
			return nil, fmt.Errorf("no handler defined for binding context type=%s binding=%s group=%s", bc.Metadata.BindingType, bc.Binding, bc.Metadata.Group)
		}

		bindingOut, err := handler(bindingInput)
		if err != nil {
			return nil, err
		}
		if bindingOut != nil && bindingOut.ConfigValuesPatches != nil {
			out.ConfigValuesPatches.MergeOperations(bindingOut.ConfigValuesPatches)
		}
		if bindingOut != nil && bindingOut.MemoryValuesPatches != nil {
			out.MemoryValuesPatches.MergeOperations(bindingOut.MemoryValuesPatches)
		}
		if bindingOut != nil && bindingOut.Metrics != nil {
			out.Metrics = append(out.Metrics, bindingOut.Metrics...)
		}
	}

	return out, nil
}
