package global_hooks

import "github.com/flant/addon-operator/sdk"

func init() {
	sdk.Register(&GoHook{})
}

type GoHook struct {
	sdk.CommonGoHook
}

func (h *GoHook) Metadata() sdk.HookMetadata {
	return h.CommonMetadataFromRuntime()
}

func (h *GoHook) Config() *sdk.HookConfig {
	return h.CommonGoHook.Config(&sdk.HookConfig{
		YamlConfig: `
configVersion: v1
onStartup: 10
`,
		MainHandler: h.Main,
	})
}

func (h *GoHook) Main(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
	input.LogEntry.Infof("Start Global Go hook")
	return nil, nil
}
