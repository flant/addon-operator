package modules

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/utils"
)

func TestMergeLayers(t *testing.T) {
	globalValues := utils.Values{
		"global": map[string]interface{}{
			"enabledModules":   []string{"module1", "module2"},
			"highAvailability": true,
		},
	}
	res := mergeLayers(
		utils.Values{},
		globalValues,
		utils.Values{
			"global": map[string]interface{}{
				"enabledModules": []string{"module3"},
				"logLevel":       "Info",
			},
		},
	)
	assert.YAMLEq(t, `
global:
  logLevel: "Info"
  highAvailability: true
  enabledModules:
  - module3
`, res.AsString("yaml"))

	assert.YAMLEq(t, `
global:
  highAvailability: true
  enabledModules:
  - module1
  - module2
`, globalValues.AsString("yaml"))
}
