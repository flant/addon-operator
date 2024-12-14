package hooks

import (
	"fmt"
	"strconv"

	sdkhook "github.com/deckhouse/module-sdk/pkg/hook"
	"github.com/go-openapi/spec"
	"sigs.k8s.io/yaml"

	. "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/shell-operator/pkg/hook/config"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
)

// ModuleHookConfig is a structure with versioned hook configuration
type ModuleHookConfig struct {
	config.HookConfig

	// versioned raw config values
	ModuleV0 *ModuleHookConfigV0
	ModuleV1 *ModuleHookConfigV0

	// effective config values
	BeforeHelm      *BeforeHelmConfig
	AfterHelm       *AfterHelmConfig
	AfterDeleteHelm *AfterDeleteHelmConfig
}

type BeforeHelmConfig struct {
	CommonBindingConfig
	Order float64
}

type AfterHelmConfig struct {
	CommonBindingConfig
	Order float64
}

type AfterDeleteHelmConfig struct {
	CommonBindingConfig
	Order float64
}

type ModuleHookConfigV0 struct {
	BeforeHelm      interface{} `json:"beforeHelm"`
	AfterHelm       interface{} `json:"afterHelm"`
	AfterDeleteHelm interface{} `json:"afterDeleteHelm"`
}

func getModuleHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "module-hook-" + version
	if _, ok := config.Schemas[globalHookVersion]; !ok {
		schema := config.Schemas[version]
		switch version {
		case "v1":
			// add beforeHelm, afterHelm and afterDeleteHelm properties
			schema += `
  beforeHelm:
    type: integer
    example: 10    
  afterHelm:
    type: integer
    example: 10    
  afterDeleteHelm:
    type: integer
    example: 10   
`
		case "v0":
			// add beforeHelm, afterHelm and afterDeleteHelm properties
			schema += `
  beforeHelm:
    type: integer
    example: 10    
  afterHelm:
    type: integer
    example: 10    
  afterDeleteHelm:
    type: integer
    example: 10    
`
		}
		config.Schemas[globalHookVersion] = schema
	}

	return config.GetSchema(globalHookVersion)
}

// LoadAndValidateShellConfig loads shell hook config from bytes and validate it. Returns multierror.
func (c *ModuleHookConfig) LoadAndValidateShellConfig(data []byte) error {
	// find config version
	vu := config.NewDefaultVersionedUntyped()
	err := vu.Load(data)
	if err != nil {
		return fmt.Errorf("load data: %w", err)
	}

	// validate config scheme
	err = config.ValidateConfig(vu.Obj, getModuleHookConfigSchema(vu.Version), "")
	if err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	c.Version = vu.Version

	// unmarshal data and enrich hook config
	err = c.HookConfig.ConvertAndCheck(data)
	if err != nil {
		return fmt.Errorf("convert and check hook config: %w", err)
	}

	// unmarshal data and enrich module hook config
	err = c.ConvertAndCheck(data)
	if err != nil {
		return fmt.Errorf("convert and check: %w", err)
	}

	return nil
}

func remapHookConfigV1FromHookConfig(hcfg *sdkhook.HookConfig) *config.HookConfigV1 {
	hcv1 := &config.HookConfigV1{
		ConfigVersion: hcfg.ConfigVersion,
	}

	if len(hcfg.Schedule) > 0 {
		hcv1.Schedule = make([]config.ScheduleConfigV1, 0, len(hcfg.Schedule))
	}

	if len(hcfg.Kubernetes) > 0 {
		hcv1.OnKubernetesEvent = make([]config.OnKubernetesEventConfigV1, 0, len(hcfg.Kubernetes))
	}

	if hcfg.OnStartup != nil {
		hcv1.OnStartup = float64(*hcfg.OnStartup)
	}

	if hcfg.Settings != nil {
		hcv1.Settings = &config.SettingsV1{
			ExecutionMinInterval: hcfg.Settings.ExecutionMinInterval.String(),
			ExecutionBurst:       strconv.Itoa(hcfg.Settings.ExecutionBurst),
		}
	}

	for _, sch := range hcfg.Schedule {
		hcv1.Schedule = append(hcv1.Schedule, config.ScheduleConfigV1{
			Name:    sch.Name,
			Crontab: sch.Crontab,
		})
	}

	for _, kube := range hcfg.Kubernetes {
		newShCfg := config.OnKubernetesEventConfigV1{
			ApiVersion:                   kube.APIVersion,
			Kind:                         kube.Kind,
			Name:                         kube.Name,
			LabelSelector:                kube.LabelSelector,
			JqFilter:                     kube.JqFilter,
			ExecuteHookOnSynchronization: "true",
			WaitForSynchronization:       "true",
			// permanently false
			KeepFullObjectsInMemory: "false",
			ResynchronizationPeriod: kube.ResynchronizationPeriod,
			IncludeSnapshotsFrom:    kube.IncludeSnapshotsFrom,
			Queue:                   kube.Queue,
			// TODO: make default constants public to use here
			// like go hooks apply default
			Group: "main",
		}

		if kube.NameSelector != nil {
			newShCfg.NameSelector = &config.KubeNameSelectorV1{
				MatchNames: kube.NameSelector.MatchNames,
			}
		}

		if kube.NamespaceSelector != nil {
			newShCfg.Namespace = &config.KubeNamespaceSelectorV1{
				NameSelector:  (*types.NameSelector)(kube.NamespaceSelector.NameSelector),
				LabelSelector: kube.NamespaceSelector.LabelSelector,
			}
		}

		if kube.FieldSelector != nil {
			fs := &config.KubeFieldSelectorV1{
				MatchExpressions: make([]types.FieldSelectorRequirement, 0, len(kube.FieldSelector.MatchExpressions)),
			}

			for _, expr := range kube.FieldSelector.MatchExpressions {
				fs.MatchExpressions = append(fs.MatchExpressions, types.FieldSelectorRequirement(expr))
			}

			newShCfg.FieldSelector = fs
		}

		if kube.KeepFullObjectsInMemory != nil {
			newShCfg.KeepFullObjectsInMemory = strconv.FormatBool(*kube.KeepFullObjectsInMemory)
		}

		// *bool --> ExecuteHookOnEvents: [All events] || empty array or nothing
		if kube.ExecuteHookOnEvents != nil && !*kube.ExecuteHookOnEvents {
			newShCfg.ExecuteHookOnEvents = make([]types.WatchEventType, 0, 1)
		}

		if kube.ExecuteHookOnSynchronization != nil {
			newShCfg.ExecuteHookOnSynchronization = strconv.FormatBool(*kube.ExecuteHookOnSynchronization)
		}

		if kube.WaitForSynchronization != nil {
			newShCfg.WaitForSynchronization = strconv.FormatBool(*kube.WaitForSynchronization)
		}

		if kube.KeepFullObjectsInMemory != nil {
			newShCfg.KeepFullObjectsInMemory = strconv.FormatBool(*kube.KeepFullObjectsInMemory)
		}

		if kube.AllowFailure != nil {
			newShCfg.AllowFailure = *kube.AllowFailure
		}

		hcv1.OnKubernetesEvent = append(hcv1.OnKubernetesEvent, newShCfg)
	}

	return hcv1
}

func (c *ModuleHookConfig) LoadAndValidateBatchConfig(hcfg *sdkhook.HookConfig) error {
	c.Version = hcfg.ConfigVersion

	hcv1 := remapHookConfigV1FromHookConfig(hcfg)

	c.HookConfig.V1 = hcv1
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
		c.BeforeHelm = &BeforeHelmConfig{}
		c.BeforeHelm.BindingName = string(BeforeHelm)
		c.BeforeHelm.Order = float64(*hcfg.OnBeforeHelm)
	}

	if hcfg.OnAfterHelm != nil {
		c.AfterHelm = &AfterHelmConfig{}
		c.AfterHelm.BindingName = string(AfterHelm)
		c.AfterHelm.Order = float64(*hcfg.OnAfterHelm)
	}

	if hcfg.OnAfterDeleteHelm != nil {
		c.AfterDeleteHelm = &AfterDeleteHelmConfig{}
		c.AfterDeleteHelm.BindingName = string(AfterDeleteHelm)
		c.AfterDeleteHelm.Order = float64(*hcfg.OnAfterDeleteHelm)
	}

	return nil
}

func (c *ModuleHookConfig) LoadAndValidateGoConfig(input *gohook.HookConfig) error {
	hookConfig, err := newHookConfigFromGoConfig(input)
	if err != nil {
		return err
	}

	c.HookConfig = hookConfig

	if input.OnBeforeHelm != nil {
		c.BeforeHelm = &BeforeHelmConfig{}
		c.BeforeHelm.BindingName = string(BeforeHelm)
		c.BeforeHelm.Order = input.OnBeforeHelm.Order
	}

	if input.OnAfterHelm != nil {
		c.AfterHelm = &AfterHelmConfig{}
		c.AfterHelm.BindingName = string(AfterHelm)
		c.AfterHelm.Order = input.OnAfterHelm.Order
	}

	if input.OnAfterDeleteHelm != nil {
		c.AfterDeleteHelm = &AfterDeleteHelmConfig{}
		c.AfterDeleteHelm.BindingName = string(AfterDeleteHelm)
		c.AfterDeleteHelm.Order = input.OnAfterDeleteHelm.Order
	}

	return nil
}

func (c *ModuleHookConfig) ConvertAndCheck(data []byte) error {
	switch c.Version {
	case "v0":
		configV0 := &ModuleHookConfigV0{}
		err := yaml.Unmarshal(data, configV0)
		if err != nil {
			return fmt.Errorf("unmarshal ModuleHookConfig version 0: %s", err)
		}
		c.ModuleV0 = configV0
		err = c.ConvertAndCheckV0()
		if err != nil {
			return err
		}
	case "v1":
		configV1 := &ModuleHookConfigV0{}
		err := yaml.Unmarshal(data, configV1)
		if err != nil {
			return fmt.Errorf("unmarshal ModuleHookConfig v1: %s", err)
		}
		c.ModuleV1 = configV1
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

func (c *ModuleHookConfig) ConvertAndCheckV0() (err error) {
	c.BeforeHelm, err = c.ConvertBeforeHelm(c.ModuleV0.BeforeHelm)
	if err != nil {
		return err
	}
	c.AfterHelm, err = c.ConvertAfterHelm(c.ModuleV0.AfterHelm)
	if err != nil {
		return err
	}
	c.AfterDeleteHelm, err = c.ConvertAfterDeleteHelm(c.ModuleV0.AfterDeleteHelm)
	if err != nil {
		return err
	}

	return nil
}

func (c *ModuleHookConfig) ConvertAndCheckV1() (err error) {
	c.BeforeHelm, err = c.ConvertBeforeHelm(c.ModuleV1.BeforeHelm)
	if err != nil {
		return err
	}
	c.AfterHelm, err = c.ConvertAfterHelm(c.ModuleV1.AfterHelm)
	if err != nil {
		return err
	}
	c.AfterDeleteHelm, err = c.ConvertAfterDeleteHelm(c.ModuleV1.AfterDeleteHelm)
	if err != nil {
		return err
	}

	return nil
}

func (c *ModuleHookConfig) ConvertBeforeHelm(value interface{}) (*BeforeHelmConfig, error) {
	floatValue, err := config.ConvertFloatForBinding(value, "beforeHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &BeforeHelmConfig{}
	res.BindingName = string(BeforeHelm)
	res.Order = *floatValue
	return res, nil
}

func (c *ModuleHookConfig) ConvertAfterHelm(value interface{}) (*AfterHelmConfig, error) {
	floatValue, err := config.ConvertFloatForBinding(value, "afterHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterHelmConfig{}
	res.BindingName = string(AfterHelm)
	res.Order = *floatValue
	return res, nil
}

func (c *ModuleHookConfig) ConvertAfterDeleteHelm(value interface{}) (*AfterDeleteHelmConfig, error) {
	floatValue, err := config.ConvertFloatForBinding(value, "afterDeleteHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterDeleteHelmConfig{}
	res.BindingName = string(AfterDeleteHelm)
	res.Order = *floatValue
	return res, nil
}

func (c *ModuleHookConfig) Bindings() []BindingType {
	res := make([]BindingType, 0)

	for _, binding := range []BindingType{OnStartup, Schedule, OnKubernetesEvent, BeforeHelm, AfterHelm, AfterDeleteHelm} {
		if c.HasBinding(binding) {
			res = append(res, binding)
		}
	}

	return res
}

func (c *ModuleHookConfig) HasBinding(binding BindingType) bool {
	if c.HookConfig.HasBinding(binding) {
		return true
	}
	switch binding {
	case BeforeHelm:
		return c.BeforeHelm != nil
	case AfterHelm:
		return c.AfterHelm != nil
	case AfterDeleteHelm:
		return c.AfterDeleteHelm != nil
	default:
		return false
	}
}

func (c *ModuleHookConfig) BindingsCount() int {
	res := 0

	for _, binding := range []BindingType{OnStartup, BeforeHelm, AfterHelm, AfterDeleteHelm} {
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

func (c *ModuleHookConfig) LoadAndValidateConfig(configLoader gohook.HookConfigLoader) error {
	err := configLoader.LoadAndValidate(&c.HookConfig, "embedded")
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
		c.BeforeHelm = &BeforeHelmConfig{}
		c.BeforeHelm.BindingName = string(BeforeHelm)
		c.BeforeHelm.Order = *beforeAll
	}

	afterAll := configLoader.LoadAfterAll()
	if afterAll != nil {
		c.AfterHelm = &AfterHelmConfig{}
		c.AfterHelm.BindingName = string(AfterHelm)
		c.AfterHelm.Order = *afterAll
	}

	afterDelete := configLoader.LoadAfterDeleteHelm()
	if afterDelete != nil {
		c.AfterDeleteHelm = &AfterDeleteHelmConfig{}
		c.AfterDeleteHelm.BindingName = string(AfterDeleteHelm)
		c.AfterDeleteHelm.Order = *afterDelete
	}

	return nil
}
