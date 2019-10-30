package module_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_GlobalHook_Config_v0_v1(t *testing.T) {
	var err error
	var config *GlobalHookConfig

	tests := []struct {
		name string
		hookName string
		data string
		assertion func()
	}{
		{
			"load v0 config",
			"hook_v0",
			`{"onStartup":10, "schedule":[{"crontab":"*/5 * * * * *"}], "beforeAll":10}`,
			func() {
				if !assert.NoError(t, err) {
					t.FailNow()
				}
			},
		},
		{
			"load v1 config",
			"hook_v1",
			`{"configVersion": "v1", "onStartup":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "afterAll":10}`,
			func() {
				if !assert.NoError(t, err) {
					t.FailNow()
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config = &GlobalHookConfig{}
			err = config.LoadAndValidate([]byte(test.data))
			test.assertion()
		})
	}
}
