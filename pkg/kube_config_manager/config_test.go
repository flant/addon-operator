package kube_config_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ParseCM_Nil(t *testing.T) {
	cfg, err := ParseConfigMapData(nil)
	assert.NoError(t, err, "Should parse nil data correctly")
	assert.NotNil(t, cfg, "Config should not be nil for nil data")
	assert.Nil(t, cfg.Global, "No global config should present for nil data")
	assert.NotNil(t, cfg.Modules, "Modules should not be nil for nil data")
	assert.Len(t, cfg.Modules, 0, "No module configs should present for nil data")
}

func Test_ParseCM_Empty(t *testing.T) {
	cfg, err := ParseConfigMapData(map[string]string{})
	assert.NoError(t, err, "Should parse empty data correctly")
	assert.NotNil(t, cfg, "Config should not be nil for empty data")
	assert.Nil(t, cfg.Global, "No global config should present for empty data")
	assert.NotNil(t, cfg.Modules, "Modules should not be nil for empty data")
	assert.Len(t, cfg.Modules, 0, "No module configs should present for empty data")
}

func Test_ParseCM_only_Global(t *testing.T) {
	cfg, err := ParseConfigMapData(map[string]string{
		"global": `
param1: val1
param2: val2
`,
	})
	assert.NoError(t, err, "Should parse global only data correctly")
	assert.NotNil(t, cfg, "Config should not be nil for global only data")
	assert.NotNil(t, cfg.Global, "Global config should present for global only data")
	assert.True(t, cfg.Global.Values.HasGlobal(), "Config should have global values for global only data")
	assert.NotNil(t, cfg.Modules, "Modules should not be nil for global only data")
	assert.Len(t, cfg.Modules, 0, "No module configs should present for global only data")

}

func Test_ParseCM_only_Modules(t *testing.T) {
	cfg, err := ParseConfigMapData(map[string]string{
		"modOne": `
param1: val1
param2: val2
`,
		"modOneEnabled": `false`,
		"modTwo": `
param1: val1
param2: val2
`,
		"modThreeEnabled": `true`,
	})
	assert.NoError(t, err, "Should parse modules only data correctly")
	assert.NotNil(t, cfg, "Config should not be nil for modules only data")
	assert.Nil(t, cfg.Global, "Global config should not present for modules only data")
	assert.NotNil(t, cfg.Modules, "Modules should not be nil for modules only data")
	assert.Len(t, cfg.Modules, 3, "Module configs should present for modules only data")

	assert.Containsf(t, cfg.Modules, "mod-one", "Config for modOne should present for modules only data")
	mod := cfg.Modules["mod-one"]
	assert.Equal(t, "modOneEnabled", mod.ModuleEnabledKey)
	assert.Equal(t, "modOne", mod.ModuleConfigKey)
	assert.Equal(t, "false", mod.GetEnabled())
	assert.Containsf(t, cfg.Modules, "mod-two", "Config for modOne should present for modules only data")
	mod = cfg.Modules["mod-two"]
	assert.Equal(t, "modTwoEnabled", mod.ModuleEnabledKey)
	assert.Equal(t, "modTwo", mod.ModuleConfigKey)
	assert.Equal(t, "n/d", mod.GetEnabled())
	assert.Containsf(t, cfg.Modules, "mod-three", "Config for modOne should present for modules only data")
	mod = cfg.Modules["mod-three"]
	assert.Equal(t, "modThreeEnabled", mod.ModuleEnabledKey)
	assert.Equal(t, "modThree", mod.ModuleConfigKey)
	assert.Equal(t, "true", mod.GetEnabled())
}

func Test_ParseCM_Malformed_Data(t *testing.T) {
	var err error

	_, err = ParseConfigMapData(map[string]string{
		"Malformed-section-name": `
param1: val1
param2: val2
`,
	})
	assert.Error(t, err, "Should parse malformed module name with error")

	_, err = ParseConfigMapData(map[string]string{
		"invalidYAML": `
param1: val1
  param2: val2
`,
	})
	assert.Error(t, err, "Should parse bad module values with error")
}
