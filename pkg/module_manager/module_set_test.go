package module_manager

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestModuleSet(t *testing.T) {
	g := NewWithT(t)
	ms := new(ModuleSet)

	ms.Add(&Module{
		Name:  "module-two",
		Order: 10,
	})
	ms.Add(&Module{
		Name:  "module-one",
		Order: 5,
	})
	ms.Add(&Module{
		Name:  "module-three-two",
		Order: 15,
	})
	ms.Add(&Module{
		Name:  "module-four",
		Order: 1,
	})
	ms.Add(&Module{
		Name:  "module-three-one",
		Order: 15,
	})
	// "overriden" module
	ms.Add(&Module{
		Name:  "module-four",
		Order: 20,
	})

	expectNames := []string{
		"module-one",
		"module-two",
		"module-three-one",
		"module-three-two",
		"module-four",
	}

	g.Expect(ms.NamesInOrder()).Should(Equal(expectNames))
	g.Expect(ms.Has("module-four")).Should(BeTrue(), "should have module-four")
	g.Expect(ms.Get("module-four").Order).Should(Equal(20), "should have module-four with order:20")
}
