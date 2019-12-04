package module_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ModuleHook_Config_v0_v1(t *testing.T) {
	var err error
	var config *ModuleHookConfig

	tests := []struct {
		name string
		hookName string
		data string
		assertion func()
	}{
		{
			"load v0 module config",
			"hook_v0",
			`{"onStartup":10, "schedule":[{"crontab":"*/5 * * * * *"}], "afterDeleteHelm": 25}`,
			func() {
				if assert.NoError(t, err) {
					assert.Len(t, config.Schedules, 1)
					assert.True(t, config.HasBinding(OnStartup))
					assert.True(t, config.HasBinding(Schedule))
					assert.True(t, config.HasBinding(AfterDeleteHelm))
					assert.False(t, config.HasBinding(KubeEvents))
					assert.False(t, config.HasBinding(AfterHelm))
					assert.False(t, config.HasBinding(BeforeHelm))
				}
			},
		},
		{
			"load v1 module config",
			"hook_v1",
			`{"configVersion": "v1", "onStartup":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "beforeHelm":98}`,
			func() {
				if assert.NoError(t, err) {
					assert.Len(t, config.OnKubernetesEvents, 1)
					assert.Len(t, config.Schedules, 0)
					assert.True(t, config.HasBinding(OnStartup))
					assert.True(t, config.HasBinding(KubeEvents))
					assert.True(t, config.HasBinding(BeforeHelm))
					assert.False(t, config.HasBinding(Schedule))
					assert.False(t, config.HasBinding(AfterDeleteHelm))
					assert.False(t, config.HasBinding(AfterHelm))
				}

			},
		},
		{
			"load v1 bad module config",
			"hook_v1",
			`{"configVersion": "v1", "onStartuppp":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "beforeHelm":98}`,
			func() {
				assert.Error(t, err)
				t.Logf("expected error: %v", err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config = &ModuleHookConfig{}
			err = config.LoadAndValidate([]byte(test.data))
			test.assertion()
		})
	}
}

