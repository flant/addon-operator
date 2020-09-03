package global_go_hook

import "github.com/flant/addon-operator/sdk"

func init() {
	sdk.Register(&Simple{})
}

type Simple struct {
}

func (s *Simple) Metadata() sdk.HookMetadata {
	return sdk.HookMetadata{
		Name:   "simple",
		Path:   "global-hooks/simple",
		Global: true,
	}
}

func (s *Simple) Config() *sdk.HookConfig {
	return &sdk.HookConfig{
		YamlConfig: `
configVersion: v1
onStartup: 10
`,
	}
}

func (s *Simple) Run(input *sdk.HookInput) (output *sdk.HookOutput, err error) {
	return &sdk.HookOutput{}, nil
}
