package module_manager

import (
	"fmt"

	"github.com/go-openapi/spec"
	"sigs.k8s.io/yaml"

	. "github.com/flant/shell-operator/pkg/hook/types"

	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"

	"github.com/flant/shell-operator/pkg/hook/config"
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

func GetModuleHookConfigSchema(version string) *spec.Schema {
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

// LoadAndValidate loads config from bytes and validate it. Returns multierror.
func (c *ModuleHookConfig) LoadAndValidate(data []byte) error {
	vu := config.NewDefaultVersionedUntyped()
	err := vu.Load(data)
	if err != nil {
		return err
	}

	err = config.ValidateConfig(vu.Obj, GetModuleHookConfigSchema(vu.Version), "")
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
	res := []BindingType{}

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
	}
	return false
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

func NewModuleHookConfigFromGoConfig(input *go_hook.HookConfig) (*ModuleHookConfig, error) {
	hookConfig, err := NewHookConfigFromGoConfig(input)
	if err != nil {
		return nil, err
	}

	cfg := &ModuleHookConfig{
		HookConfig: hookConfig,
	}

	if input.OnBeforeHelm != nil {
		cfg.BeforeHelm = &BeforeHelmConfig{}
		cfg.BeforeHelm.BindingName = string(BeforeHelm)
		cfg.BeforeHelm.Order = input.OnBeforeHelm.Order
	}

	if input.OnAfterHelm != nil {
		cfg.AfterHelm = &AfterHelmConfig{}
		cfg.AfterHelm.BindingName = string(AfterHelm)
		cfg.AfterHelm.Order = input.OnAfterHelm.Order
	}

	if input.OnAfterDeleteHelm != nil {
		cfg.AfterDeleteHelm = &AfterDeleteHelmConfig{}
		cfg.AfterDeleteHelm.BindingName = string(AfterDeleteHelm)
		cfg.AfterDeleteHelm.Order = input.OnAfterDeleteHelm.Order
	}

	return cfg, nil
}
