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
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-one",
		Order: 5,
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-three-two",
		Order: 15,
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-four",
		Order: 1,
	})
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-three-one",
		Order: 15,
	})
	// "overridden" BasicModule
	ms.Add(&modules.BasicModule{
		Name:  "BasicModule-four",
		Order: 20,
	})

	expectNames := []string{
		"BasicModule-one",
		"BasicModule-two",
		"BasicModule-three-one",
		"BasicModule-three-two",
		"BasicModule-four",
	}

	g.Expect(ms.NamesInOrder()).Should(Equal(expectNames))
	g.Expect(ms.Has("BasicModule-four")).Should(BeTrue(), "should have BasicModule-four")
	g.Expect(ms.Get("BasicModule-four").Order).Should(Equal(20), "should have BasicModule-four with order:20")
}
