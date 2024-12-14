package hooks

import (
	"errors"
	"fmt"

	"github.com/davecgh/go-spew/spew"
	sdkhook "github.com/deckhouse/module-sdk/pkg/hook"
	"github.com/go-openapi/spec"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	. "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/shell-operator/pkg/hook/config"
	. "github.com/flant/shell-operator/pkg/hook/types"
	kubeeventsmanager "github.com/flant/shell-operator/pkg/kube_events_manager"
	eventtypes "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	schdulertypes "github.com/flant/shell-operator/pkg/schedule_manager/types"
)

const (
	defaultHookGroupName = "main"
	defaultHookQueueName = "main"
)

// GlobalHookConfig is a structure with versioned hook configuration
type GlobalHookConfig struct {
	config.HookConfig

	// versioned raw config values
	GlobalV0 *GlobalHookConfigV0
	GlobalV1 *GlobalHookConfigV0

	// effective config values
	BeforeAll *BeforeAllConfig
	AfterAll  *AfterAllConfig
}

type BeforeAllConfig struct {
	CommonBindingConfig
	Order float64
}

type AfterAllConfig struct {
	CommonBindingConfig
	Order float64
}

type GlobalHookConfigV0 struct {
	BeforeAll interface{} `json:"beforeAll"`
	AfterAll  interface{} `json:"afterAll"`
}

func getGlobalHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "global-hook-" + version
	if _, ok := config.Schemas[globalHookVersion]; !ok {
		schema := config.Schemas[version]
		switch version {
		case "v1":
			// add beforeAll and afterAll properties
			schema += `
  beforeAll:
    type: integer
    example: 10    
  afterAll:
    type: integer
    example: 10    
`
		case "v0":
			// add beforeAll and afterAll properties
			schema += `
  beforeAll:
    type: integer
    example: 10    
  afterAll:
    type: integer
    example: 10    
`
		}
		config.Schemas[globalHookVersion] = schema
	}

	return config.GetSchema(globalHookVersion)
}

// LoadAndValidateShellConfig loads shell hook config from bytes and validate it. Returns multierror.
func (c *GlobalHookConfig) LoadAndValidateShellConfig(data []byte) error {
	vu := config.NewDefaultVersionedUntyped()
	err := vu.Load(data)
	if err != nil {
		return err
	}

	err = config.ValidateConfig(vu.Obj, getGlobalHookConfigSchema(vu.Version), "")
	if err != nil {
		return err
	}

	c.Version = vu.Version

	err = c.HookConfig.ConvertAndCheck(data)
	if err != nil {
		return err
	}

	err = c.ConvertAndCheck(data)
	if err != nil {
		return err
	}

	return nil
}

func (c *GlobalHookConfig) LoadAndValidateBatchConfig(hcfg *sdkhook.HookConfig) error {
	c.Version = hcfg.ConfigVersion

	hcv1 := remapHookConfigV1FromHookConfig(hcfg)

	err := hcv1.ConvertAndCheck(&c.HookConfig)
	if err != nil {
		return fmt.Errorf("convert and check from hook config v1: %w", err)
	}

	if hcfg.OnStartup != nil {
		c.OnStartup = &OnStartupConfig{}
		c.OnStartup.AllowFailure = false
		c.OnStartup.BindingName = string(OnStartup)
		c.OnStartup.Order = float64(*hcfg.OnStartup)
	}

	if hcfg.OnBeforeHelm != nil {
		c.BeforeAll = &BeforeAllConfig{}
		c.BeforeAll.BindingName = string(BeforeAll)
		c.BeforeAll.Order = float64(*hcfg.OnBeforeHelm)
	}

	if hcfg.OnAfterHelm != nil {
		c.AfterAll = &AfterAllConfig{}
		c.AfterAll.BindingName = string(AfterAll)
		c.AfterAll.Order = float64(*hcfg.OnAfterHelm)
	}

	return nil
}

func (c *GlobalHookConfig) LoadAndValidateGoConfig(input *gohook.HookConfig) error {
	hookConfig, err := newHookConfigFromGoConfig(input)
	if err != nil {
		return err
	}

	c.HookConfig = hookConfig

	if input.OnBeforeAll != nil {
		c.BeforeAll = &BeforeAllConfig{}
		c.BeforeAll.BindingName = string(BeforeAll)
		c.BeforeAll.Order = input.OnBeforeAll.Order
	}

	if input.OnAfterAll != nil {
		c.AfterAll = &AfterAllConfig{}
		c.AfterAll.BindingName = string(AfterAll)
		c.AfterAll.Order = input.OnAfterAll.Order
	}

	return nil
}

func (c *GlobalHookConfig) ConvertAndCheck(data []byte) error {
	switch c.Version {
	case "v0":
		configV0 := &GlobalHookConfigV0{}
		err := yaml.Unmarshal(data, configV0)
		if err != nil {
			return fmt.Errorf("unmarshal GlobalHookConfig version 0: %s", err)
		}
		c.GlobalV0 = configV0
		err = c.ConvertAndCheckV0()
		if err != nil {
			return err
		}
	case "v1":
		configV1 := &GlobalHookConfigV0{}
		err := yaml.Unmarshal(data, configV1)
		if err != nil {
			return fmt.Errorf("unmarshal GlobalHookConfig v1: %s", err)
		}
		c.GlobalV1 = configV1
		err = c.ConvertAndCheckV1()
		if err != nil {
			return err
		}
	default:
		// NOTE: this should not happen
		return fmt.Errorf("version '%s' is unsupported", c.Version)
	}

	return nil
}

func (c *GlobalHookConfig) ConvertAndCheckV0() (err error) {
	c.BeforeAll, err = c.ConvertBeforeAll(c.GlobalV0.BeforeAll)
	if err != nil {
		return err
	}

	c.AfterAll, err = c.ConvertAfterAll(c.GlobalV0.AfterAll)
	if err != nil {
		return err
	}

	return nil
}

func (c *GlobalHookConfig) ConvertAndCheckV1() (err error) {
	c.BeforeAll, err = c.ConvertBeforeAll(c.GlobalV1.BeforeAll)
	if err != nil {
		return err
	}

	c.AfterAll, err = c.ConvertAfterAll(c.GlobalV1.AfterAll)
	if err != nil {
		return err
	}

	return nil
}

func (c *GlobalHookConfig) ConvertBeforeAll(value interface{}) (*BeforeAllConfig, error) {
	floatValue, err := config.ConvertFloatForBinding(value, "beforeAll")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &BeforeAllConfig{}
	res.BindingName = string(BeforeAll)
	res.Order = *floatValue
	return res, nil
}

func (c *GlobalHookConfig) ConvertAfterAll(value interface{}) (*AfterAllConfig, error) {
	floatValue, err := config.ConvertFloatForBinding(value, "afterAll")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterAllConfig{}
	res.BindingName = string(AfterAll)
	res.Order = *floatValue
	return res, nil
}

func (c *GlobalHookConfig) Bindings() []BindingType {
	res := make([]BindingType, 0)

	for _, binding := range []BindingType{OnStartup, Schedule, OnKubernetesEvent, BeforeAll, AfterAll} {
		if c.HasBinding(binding) {
			res = append(res, binding)
		}
	}

	return res
}

func (c *GlobalHookConfig) HasBinding(binding BindingType) bool {
	if c.HookConfig.HasBinding(binding) {
		return true
	}
	switch binding {
	case BeforeAll:
		return c.BeforeAll != nil
	case AfterAll:
		return c.AfterAll != nil
	}
	return false
}

func (c *GlobalHookConfig) BindingsCount() int {
	res := 0

	for _, binding := range []BindingType{OnStartup, BeforeAll, AfterAll} {
		if c.HasBinding(binding) {
			res++
		}
	}

	if c.HasBinding(Schedule) {
		res += len(c.Schedules)
	}
	if c.HasBinding(OnKubernetesEvent) {
		res += len(c.OnKubernetesEvents)
	}
	return res
}

func newHookConfigFromGoConfig(input *gohook.HookConfig) (config.HookConfig, error) {
	c := config.HookConfig{
		Version:            "v1",
		Schedules:          []ScheduleConfig{},
		OnKubernetesEvents: []OnKubernetesEventConfig{},
	}

	if input.Settings != nil {
		c.Settings = &Settings{
			ExecutionMinInterval: input.Settings.ExecutionMinInterval,
			ExecutionBurst:       input.Settings.ExecutionBurst,
		}
	}

	if input.OnStartup != nil {
		c.OnStartup = &OnStartupConfig{}
		c.OnStartup.BindingName = string(OnStartup)
		c.OnStartup.Order = input.OnStartup.Order
	}

	/*** A HUGE copy paste from shell-operator’s hook_config.ConvertAndCheckV1   ***/
	// WARNING no checks and defaults!
	for i, kubeCfg := range input.Kubernetes {
		// err := c.CheckOnKubernetesEventV1(kubeCfg, fmt.Sprintf("kubernetes[%d]", i))
		// if err != nil {
		//	return fmt.Errorf("invalid kubernetes config [%d]: %v", i, err)
		//}

		monitor := &kubeeventsmanager.MonitorConfig{}
		monitor.Metadata.DebugName = config.MonitorDebugName(kubeCfg.Name, i)
		monitor.Metadata.MonitorId = config.MonitorConfigID()
		monitor.Metadata.LogLabels = map[string]string{}
		monitor.Metadata.MetricLabels = map[string]string{}
		// monitor.WithMode(kubeCfg.Mode)
		monitor.ApiVersion = kubeCfg.ApiVersion
		monitor.Kind = kubeCfg.Kind
		monitor.WithNameSelector(kubeCfg.NameSelector)
		monitor.WithFieldSelector(kubeCfg.FieldSelector)
		monitor.WithNamespaceSelector(kubeCfg.NamespaceSelector)
		monitor.WithLabelSelector(kubeCfg.LabelSelector)
		if kubeCfg.FilterFunc == nil {
			return config.HookConfig{}, errors.New(`"FilterFunc" in KubernetesConfig cannot be nil`)
		}
		filterFunc := kubeCfg.FilterFunc
		monitor.FilterFunc = func(obj *unstructured.Unstructured) (interface{}, error) {
			return filterFunc(obj)
		}
		if gohook.BoolDeref(kubeCfg.ExecuteHookOnEvents, true) {
			monitor.WithEventTypes(nil)
		} else {
			monitor.WithEventTypes([]eventtypes.WatchEventType{})
		}

		kubeConfig := OnKubernetesEventConfig{}
		kubeConfig.Monitor = monitor
		kubeConfig.AllowFailure = input.AllowFailure
		if kubeCfg.Name == "" {
			return c, spew.Errorf(`"name" is a required field in binding: %v`, kubeCfg)
		}
		kubeConfig.BindingName = kubeCfg.Name
		if input.Queue == "" {
			kubeConfig.Queue = defaultHookQueueName
		} else {
			kubeConfig.Queue = input.Queue
		}
		kubeConfig.Group = defaultHookGroupName

		kubeConfig.ExecuteHookOnSynchronization = gohook.BoolDeref(kubeCfg.ExecuteHookOnSynchronization, true)
		kubeConfig.WaitForSynchronization = gohook.BoolDeref(kubeCfg.WaitForSynchronization, true)

		kubeConfig.KeepFullObjectsInMemory = false
		kubeConfig.Monitor.KeepFullObjectsInMemory = false

		c.OnKubernetesEvents = append(c.OnKubernetesEvents, kubeConfig)
	}

	// for i, kubeCfg := range c.V1.OnKubernetesEvent {
	//	if len(kubeCfg.IncludeSnapshotsFrom) > 0 {
	//		err := c.CheckIncludeSnapshots(kubeCfg.IncludeSnapshotsFrom...)
	//		if err != nil {
	//			return fmt.Errorf("invalid kubernetes config [%d]: includeSnapshots %v", i, err)
	//		}
	//	}
	//}

	// schedule bindings with includeSnapshotsFrom
	// are depend on kubernetes bindings.
	c.Schedules = []ScheduleConfig{}
	for _, inSch := range input.Schedule {
		// err := c.CheckScheduleV1(rawSchedule)
		// if err != nil {
		//	return fmt.Errorf("invalid schedule config [%d]: %v", i, err)
		//}

		res := ScheduleConfig{}

		if inSch.Name == "" {
			return c, spew.Errorf(`"name" is a required field in binding: %v`, inSch)
		}
		res.BindingName = inSch.Name

		res.AllowFailure = input.AllowFailure
		res.ScheduleEntry = schdulertypes.ScheduleEntry{
			Crontab: inSch.Crontab,
			Id:      config.ScheduleID(),
		}

		if input.Queue == "" {
			res.Queue = "main"
		} else {
			res.Queue = input.Queue
		}
		res.Group = "main"

		// schedule, err := c.ConvertScheduleV1(rawSchedule)
		// if err != nil {
		//	return err
		//}
		c.Schedules = append(c.Schedules, res)
	}

	// Update IncludeSnapshotsFrom for every binding with a group.
	// Merge binding's IncludeSnapshotsFrom with snapshots list calculated for group.
	groupSnapshots := make(map[string][]string)
	for _, kubeCfg := range c.OnKubernetesEvents {
		if kubeCfg.Group == "" {
			continue
		}
		if _, ok := groupSnapshots[kubeCfg.Group]; !ok {
			groupSnapshots[kubeCfg.Group] = make([]string, 0)
		}
		groupSnapshots[kubeCfg.Group] = append(groupSnapshots[kubeCfg.Group], kubeCfg.BindingName)
	}
	newKubeEvents := make([]OnKubernetesEventConfig, 0)
	for _, cfg := range c.OnKubernetesEvents {
		if snapshots, ok := groupSnapshots[cfg.Group]; ok {
			cfg.IncludeSnapshotsFrom = config.MergeArrays(cfg.IncludeSnapshotsFrom, snapshots)
		}
		newKubeEvents = append(newKubeEvents, cfg)
	}
	c.OnKubernetesEvents = newKubeEvents
	newSchedules := make([]ScheduleConfig, 0)
	for _, cfg := range c.Schedules {
		if snapshots, ok := groupSnapshots[cfg.Group]; ok {
			cfg.IncludeSnapshotsFrom = config.MergeArrays(cfg.IncludeSnapshotsFrom, snapshots)
		}
		newSchedules = append(newSchedules, cfg)
	}
	c.Schedules = newSchedules

	/*** END Copy Paste ***/

	return c, nil
}

func (c *GlobalHookConfig) LoadAndValidateConfig(configLoader gohook.HookConfigLoader) error {
	err := configLoader.LoadAndValidate(&c.HookConfig, "global")
	if err != nil {
		return fmt.Errorf("load and validate: %w", err)
	}

	onStartup := configLoader.LoadOnStartup()
	if onStartup != nil {
		c.OnStartup = &OnStartupConfig{}
		c.OnStartup.AllowFailure = false
		c.OnStartup.BindingName = string(OnStartup)
		c.OnStartup.Order = *onStartup
	}

	beforeAll := configLoader.LoadBeforeAll()
	if beforeAll != nil {
		c.BeforeAll = &BeforeAllConfig{}
		c.BeforeAll.BindingName = string(BeforeAll)
		c.BeforeAll.Order = *beforeAll
	}

	afterAll := configLoader.LoadAfterAll()
	if afterAll != nil {
		c.AfterAll = &AfterAllConfig{}
		c.AfterAll.BindingName = string(AfterAll)
		c.AfterAll.Order = *afterAll
	}

	return nil
}
