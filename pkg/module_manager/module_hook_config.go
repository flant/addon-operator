package module_manager

import (
	"fmt"

	"github.com/go-openapi/spec"
	"sigs.k8s.io/yaml"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/hook/types"

	sh_op_hook "github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/hook/config"
)

// ModuleHookConfig is a structure with versioned hook configuration
type ModuleHookConfig struct {
	sh_op_hook.HookConfig

	// versioned raw config values
	ModuleV0 *ModuleHookConfigV0
	ModuleV1 *ModuleHookConfigV0

	// effective config values
	BeforeHelm *BeforeHelmConfig
	AfterHelm *AfterHelmConfig
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
	BeforeHelm interface{} `json:"beforeHelm"`
	AfterHelm  interface{} `json:"afterHelm"`
	AfterDeleteHelm  interface{} `json:"afterDeleteHelm"`
}

func GetModuleHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "module-hook-"+version
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
	floatValue, err := sh_op_hook.ConvertFloatForBinding(value, "beforeHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &BeforeHelmConfig{}
	res.BindingName = ContextBindingType[BeforeHelm]
	res.Order = *floatValue
	return res, nil
}

func (c *ModuleHookConfig) ConvertAfterHelm(value interface{}) (*AfterHelmConfig, error) {
	floatValue, err := sh_op_hook.ConvertFloatForBinding(value, "afterHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterHelmConfig{}
	res.BindingName = ContextBindingType[AfterHelm]
	res.Order = *floatValue
	return res, nil
}

func (c *ModuleHookConfig) ConvertAfterDeleteHelm(value interface{}) (*AfterDeleteHelmConfig, error) {
	floatValue, err := sh_op_hook.ConvertFloatForBinding(value, "afterDeleteHelm")
	if err != nil || floatValue == nil {
		return nil, err
	}

	res := &AfterDeleteHelmConfig{}
	res.BindingName = ContextBindingType[AfterDeleteHelm]
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
