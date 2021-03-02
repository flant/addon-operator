package test

import (
	"testing"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"

	// Register hooks
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/global-hooks"
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/modules/001-module-one/hooks"
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/modules/002-module-two/hooks/level1/sublevel"

	. "github.com/onsi/gomega"
)

func Test_HookMetadata_from_runtime(t *testing.T) {
	g := NewWithT(t)

	hookList := sdk.Registry().Hooks()

	g.Expect(len(hookList)).Should(Equal(3))

	hooks := map[string]go_hook.HookMetadata{}

	for _, h := range hookList {
		hm := h.Metadata()
		hooks[hm.Name] = *hm
	}

	hm, ok := hooks["go-hook.go"]
	g.Expect(ok).To(BeTrue(), "global go-hook.go should be registered")
	g.Expect(hm.Global).To(BeTrue())
	g.Expect(hm.Module).To(BeFalse())
	g.Expect(hm.Path).To(Equal("go-hook.go"))

	hm, ok = hooks["module-one-hook.go"]
	g.Expect(ok).To(BeTrue(), "module-one-hook.go should be registered")
	g.Expect(hm.Global).To(BeFalse())
	g.Expect(hm.Module).To(BeTrue())
	g.Expect(hm.ModuleName).To(Equal("module-one"))
	g.Expect(hm.Path).To(Equal("001-module-one/hooks/module-one-hook.go"))

	hm, ok = hooks["sub-sub-hook.go"]
	g.Expect(ok).To(BeTrue(), "sub-sub-hook.go should be registered")
	g.Expect(hm.Global).To(BeFalse())
	g.Expect(hm.Module).To(BeTrue())
	g.Expect(hm.ModuleName).To(Equal("module-two"))
	g.Expect(hm.Path).To(Equal("002-module-two/hooks/level1/sublevel/sub-sub-hook.go"))
}
