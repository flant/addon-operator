package kind

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/unilogger"
)

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	gh := NewGoHook(&go_hook.HookConfig{
		OnAfterAll: &go_hook.OrderedConfig{Order: 5},
	}, func(input *go_hook.HookInput) error {
		input.Values.Set("test", "test")
		input.MetricsCollector.Set("test", 1.0, nil)

		return nil
	}, unilogger.NewNop())

	bc := make([]BindingContext, 0)

	res, err := gh.Execute("", bc, "", nil, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Patches).ShouldNot(BeEmpty())
	g.Expect(res.Metrics).ShouldNot(BeEmpty())
}
