package module_manager

import (
	"testing"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	. "github.com/onsi/gomega"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"

	_ "github.com/flant/addon-operator/pkg/module_manager/test/go_hooks/global-hooks"
)

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	moduleManager := NewMainModuleManager()

	var goHook go_hook.GoHook
	gh := NewGlobalHook("simple.go", "simple.go")
	for _, hookWithMeta := range sdk.Registry().Hooks() {
		if hookWithMeta.Metadata.Name == "simple.go" && hookWithMeta.Metadata.Path == "simple.go" {
			goHook = hookWithMeta.Hook
			break
		}
	}

	g.Expect(goHook).ToNot(BeNil())

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
