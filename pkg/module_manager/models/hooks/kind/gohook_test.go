package kind

import (
	"testing"

	sdkpkg "github.com/deckhouse/module-sdk/pkg"
	. "github.com/onsi/gomega"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
)

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	gh := NewGoHook(&gohook.HookConfig{
		OnAfterAll: &gohook.OrderedConfig{Order: 5},
	}, func(input *sdkpkg.HookInput) error {
		input.Values.Set("test", "test")
		input.MetricsCollector.Set("test", 1.0, nil)

		return nil
	})

	bc := make([]BindingContext, 0)

	res, err := gh.Execute("", bc, "", nil, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Patches).ShouldNot(BeEmpty())
	g.Expect(res.Metrics).ShouldNot(BeEmpty())
}
