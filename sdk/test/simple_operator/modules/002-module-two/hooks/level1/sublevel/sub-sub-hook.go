package sublevel

import "github.com/flant/addon-operator/sdk"

func init() {
	sdk.Register(&SubSubHook{})
}

type SubSubHook struct {
	sdk.CommonGoHook
}

func (h *SubSubHook) Metadata() sdk.HookMetadata {
	return h.CommonMetadataFromRuntime()
}

func (h *SubSubHook) Config() *sdk.HookConfig {
	return h.CommonGoHook.Config(&sdk.HookConfig{
		YamlConfig: `
configVersion: v1
`,
	})
}
