package moduleset

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
)

func TestBasicModuleSet(t *testing.T) {
	g := NewWithT(t)
	ms := new(ModulesSet)

	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-two",
		Order: 10,
		Config: &modules.Config{
			Name:   "BasicModule-two",
			Weight: 10,
		},
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-one",
		Order: 5,
		Config: &modules.Config{
			Name:   "BasicModule-one",
			Weight: 5,
		},
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-three-two",
		Order: 15,
		Config: &modules.Config{
			Name:   "BasicModule-three-two",
			Weight: 15,
		},
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-four",
		Order: 1,
		Config: &modules.Config{
			Name:   "BasicModule-four",
			Weight: 1,
		},
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-three-one",
		Order: 15,
		Config: &modules.Config{
			Name:   "BasicModule-three-one",
			Weight: 15,
		},
	})
	// "overridden" BasicModule
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-four",
		Order: 20,
		Config: &modules.Config{
			Name:   "BasicModule-four",
			Weight: 20,
		},
	})
	ms.SetInited()

	expectNames := []string{
		"BasicModule-one",
		"BasicModule-two",
		"BasicModule-three-one",
		"BasicModule-three-two",
		"BasicModule-four",
	}

	g.Expect(ms.NamesInOrder()).Should(Equal(expectNames))
	g.Expect(ms.Has("BasicModule-four")).Should(BeTrue(), "should have BasicModule-four")
	g.Expect(ms.Get("BasicModule-four").Order).Should(Equal(uint32(20)), "should have BasicModule-four with order:20")
	g.Expect(ms.IsInited()).Should(BeTrue(), "should be inited")
}
