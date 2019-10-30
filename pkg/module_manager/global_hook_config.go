package module_manager

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/spec"

	hook_config "github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/hook/config"
)

// GlobalHookConfig is a structure with versioned hook configuration
type GlobalHookConfig struct {
	hook_config.HookConfig

	// versioned raw config values
	GlobalV0 *GlobalHookConfigV0
	GlobalV1 *GlobalHookConfigV0

	// effective config values
	BeforeAll *BeforeAllConfig
	AfterAll *AfterAllConfig
}

type BeforeAllConfig struct {
	hook_config.CommonBindingConfig
	Order float64
}

type AfterAllConfig struct {
	hook_config.CommonBindingConfig
	Order float64
}

type GlobalHookConfigV0 struct {
	BeforeAll interface{} `json:"beforeAll"`
	AfterAll  interface{} `json:"afterAll"`
}

func GetGlobalHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "global-hook-"+version
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

// LoadAndValidate loads config from bytes and validate it. Returns multierror.
func (c *GlobalHookConfig) LoadAndValidate(data []byte) error {
	vu := config.NewDefaultVersionedUntyped()
	err := vu.Load(data)
	if err != nil {
		return err
	}

	err = config.ValidateConfig(vu.Obj, GetGlobalHookConfigSchema(vu.Version), "")
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

func (c *GlobalHookConfig) ConvertAndCheck(data []byte) error {
	switch c.Version {
	case "v0":
		configV0 := &GlobalHookConfigV0{}
		err := json.Unmarshal(data, configV0)
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
		err := json.Unmarshal(data, configV1)
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
	floatValue, err := hook_config.ConvertFloatForBinding(value, "beforeAll")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &BeforeAllConfig{}
	res.ConfigName = ContextBindingType[BeforeAll]
	res.Order = *floatValue
	return res, nil
}

func (c *GlobalHookConfig) ConvertAfterAll(value interface{}) (*AfterAllConfig, error) {
	floatValue, err := hook_config.ConvertFloatForBinding(value, "afterAll")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterAllConfig{}
	res.ConfigName = ContextBindingType[AfterAll]
	res.Order = *floatValue
	return res, nil
}

func (c *GlobalHookConfig) Bindings() []BindingType {
	res := []BindingType{}

	for _, binding := range []BindingType{OnStartup, Schedule, KubeEvents} {
		if c.HookConfig.HasBinding(ShOpBindingType[binding]) {
			res = append(res, binding)
		}
	}

	for _, binding := range []BindingType{BeforeAll, AfterAll} {
		if c.HasBinding(binding) {
			res = append(res, binding)
		}
	}

	return res
}

func (c *GlobalHookConfig) HasBinding(binding BindingType) bool {
	if c.HookConfig.HasBinding(ShOpBindingType[binding]) {
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
