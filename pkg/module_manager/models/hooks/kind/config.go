package kind

import sdkhook "github.com/deckhouse/module-sdk/pkg/hook"

const BatchHookReadyKey = "ready"

type BatchHookConfig struct {
	Hooks     map[string]*sdkhook.HookConfig
	Readiness *sdkhook.HookConfig
}
