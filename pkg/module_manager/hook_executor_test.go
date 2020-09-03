package module_manager

import (
	"testing"

	. "github.com/onsi/gomega"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/addon-operator/sdk/registry"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"
)

type SimpleHook struct {
}

func (s *SimpleHook) Metadata() sdk.HookMetadata {
	return sdk.HookMetadata{
		Name:   "simple",
		Path:   "simple",
		Global: true,
	}
}

func (s *SimpleHook) Config() (config *sdk.HookConfig) {
	return &sdk.HookConfig{
		YamlConfig: `
configVersion: v1
onStartup: 10
`,
	}
}

func (s *SimpleHook) Run(input *sdk.HookInput) (output *sdk.HookOutput, err error) {
	return &sdk.HookOutput{
		MemoryValuesPatches: new(utils.ValuesPatch),
		Metrics: []metric_operation.MetricOperation{
			metric_operation.MetricOperation{},
		},
		Error: nil,
	}, nil
}

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	goHook := &SimpleHook{}

	goHookRegistry := registry.NewRegistry()
	goHookRegistry.Add(goHook)

	moduleManager := NewMainModuleManager()

	gh := NewGlobalHook("simple", "simple")
	gh.WithGoHook(goHook)
	err := gh.WithGoConfig(goHook.Config())
	g.Expect(err).ShouldNot(HaveOccurred())
	gh.WithModuleManager(moduleManager)

	bc := []BindingContext{}

	e := NewHookExecutor(gh, bc, "v1")
	p, m, err := e.Run()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(p).ShouldNot(BeEmpty())
	g.Expect(m).ShouldNot(BeEmpty())
}
