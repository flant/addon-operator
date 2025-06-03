package kind

import sdkhook "github.com/deckhouse/module-sdk/pkg/hook"

type BatchHookConfig struct {
	Hooks     []sdkhook.HookConfig
	Readiness *sdkhook.HookConfig
}
