package module_manager

import (
	"testing"

	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/metric_storage/operation"
	. "github.com/onsi/gomega"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

type SimpleHook struct {
}

func (s *SimpleHook) Metadata() *go_hook.HookMetadata {
	return &go_hook.HookMetadata{
		Name:   "simple",
		Path:   "simple",
		Global: true,
	}
}

func (s *SimpleHook) Config() (config *go_hook.HookConfig) {
	return &go_hook.HookConfig{
		OnStartup: &go_hook.OrderedConfig{Order: 10},
	}
}

func (s *SimpleHook) Run(input *go_hook.HookInput) error {
	*input.Metrics = append(*input.Metrics, operation.MetricOperation{})

	return nil
}

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	goHook := &SimpleHook{}

	goHookRegistry := sdk.Registry()
	goHookRegistry.Add(goHook)

	moduleManager := NewMainModuleManager()

	gh := NewGlobalHook("simple", "simple")
	gh.WithGoHook(goHook)
	err := gh.WithGoConfig(goHook.Config())
	g.Expect(err).ShouldNot(HaveOccurred())
	gh.WithModuleManager(moduleManager)

	bc := []BindingContext{}

	e := NewHookExecutor(gh, bc, "v1", nil)
	res, err := e.Run()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Patches).ShouldNot(BeEmpty())
	g.Expect(res.Metrics).ShouldNot(BeEmpty())
}
