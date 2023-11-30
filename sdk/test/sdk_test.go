package test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/sdk"
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/global-hooks"
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/modules/001-module-one/hooks"
	_ "github.com/flant/addon-operator/sdk/test/simple_operator/modules/002-module-two/hooks/level1/sublevel"
)

func Test_HookMetadata_from_runtime(t *testing.T) {
	g := NewWithT(t)

	hookList := sdk.Registry().Hooks()
	g.Expect(len(hookList)).Should(Equal(3))

	globalHooks := sdk.Registry().GetGlobalHooks()
	g.Expect(len(globalHooks)).Should(Equal(1))

	hooks := map[string]*kind.GoHook{}

	for _, h := range hookList {
		hooks[h.GetName()] = h
	}

	hm, ok := hooks["go-hook.go"]
	g.Expect(ok).To(BeTrue(), "global go-hook.go should be registered")
	g.Expect(hm.GetPath()).To(Equal("/global-hooks/go-hook.go"))

	hm, ok = hooks["001-module-one/hooks/module-one-hook.go"]
	g.Expect(ok).To(BeTrue(), "module-one-hook.go should be registered")
	g.Expect(hm.GetPath()).To(Equal("/modules/001-module-one/hooks/module-one-hook.go"))

	hm, ok = hooks["002-module-two/hooks/level1/sublevel/sub-sub-hook.go"]
	g.Expect(ok).To(BeTrue(), "sub-sub-hook.go should be registered")
	g.Expect(hm.GetPath()).To(Equal("/modules/002-module-two/hooks/level1/sublevel/sub-sub-hook.go"))
}
