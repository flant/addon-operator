package module_manager

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	_ "github.com/flant/addon-operator/pkg/module_manager/test/go_hooks/global-hooks"
	"github.com/flant/addon-operator/sdk"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
)

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	moduleManager := NewModuleManager()

	expectedGoHookName := "simple.go"
	expectedGoHookPath := "/global-hooks/simple.go"

	var goHook go_hook.GoHook
	gh := NewGlobalHook(expectedGoHookName, expectedGoHookPath)
	for _, hookWithMeta := range sdk.Registry().Hooks() {
		if hookWithMeta.Metadata.Name == expectedGoHookName && hookWithMeta.Metadata.Path == expectedGoHookPath {
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
