package hooks

import (
	"fmt"

	. "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/shell-operator/pkg/hook/config"
	. "github.com/flant/shell-operator/pkg/hook/types"
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

func (c *GlobalHookConfig) LoadHookConfig(configLoader gohook.HookConfigLoader) error {
	cfg, err := configLoader.GetConfigForModule("global")
	if err != nil {
		return fmt.Errorf("load and validate: %w", err)
	}

	c.HookConfig = *cfg

	onStartup := configLoader.GetOnStartup()
	if onStartup != nil {
		c.OnStartup = &OnStartupConfig{}
		c.OnStartup.AllowFailure = false
		c.OnStartup.BindingName = string(OnStartup)
		c.OnStartup.Order = *onStartup
	}

	beforeAll := configLoader.GetBeforeAll()
	if beforeAll != nil {
		c.BeforeAll = &BeforeAllConfig{}
		c.BeforeAll.BindingName = string(BeforeAll)
		c.BeforeAll.Order = *beforeAll
	}

	afterAll := configLoader.GetAfterAll()
	if afterAll != nil {
		c.AfterAll = &AfterAllConfig{}
		c.AfterAll.BindingName = string(AfterAll)
		c.AfterAll.Order = *afterAll
	}

	return nil
}
