package hooks

import (
	"fmt"

	. "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/shell-operator/pkg/hook/config"
	. "github.com/flant/shell-operator/pkg/hook/types"
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
