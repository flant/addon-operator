package hooks

import "github.com/flant/addon-operator/sdk"

func init() {
	sdk.Register(&ModuleOneHook{})
}

type ModuleOneHook struct {
	sdk.CommonGoHook
}

func (h *ModuleOneHook) Metadata() sdk.HookMetadata {
	return h.CommonMetadataFromRuntime()
}

func (h *ModuleOneHook) Config() *sdk.HookConfig {
	return h.CommonGoHook.Config(&sdk.HookConfig{
		YamlConfig: `
configVersion: v1
`,
	})
}
