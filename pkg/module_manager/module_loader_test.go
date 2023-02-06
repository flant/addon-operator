package module_manager

import (
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestDirWithSymlinks(t *testing.T) {
	g := NewWithT(t)
	dir := "testdata/module_loader/dir1"

	mods, err := SearchModules(dir)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods.Has("module-one")).Should(BeTrue())
	g.Expect(mods.Has("module-two")).Should(BeTrue())

	vals, err := LoadCommonStaticValues(dir)
	g.Expect(err).ShouldNot(HaveOccurred(), "should load common values")
	g.Expect(vals).Should(MatchAllKeys(Keys{
		"moduleOne": MatchAllKeys(Keys{
			"param1": Equal("val1"),
			"param2": Equal("val2"),
		}),
		"moduleTwo": MatchAllKeys(Keys{
			"param1": Equal("val1"),
		}),
	}), "should load values for module-one and module-two")
}

func TestLoadMultiDir(t *testing.T) {
	g := NewWithT(t)
	dirs := "testdata/module_loader/dir2:testdata/module_loader/dir3"

	mods, err := SearchModules(dirs)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods.Has("module-one")).ShouldNot(BeTrue(), "should not load module-one")
	g.Expect(mods.Has("module-two")).ShouldNot(BeTrue())

	g.Expect(mods.Has("mod-one")).Should(BeTrue(), "should load module-one as mod-one")
	g.Expect(mods.Has("mod-two")).Should(BeTrue(), "should load module-one as mod-two")

	vals, err := LoadCommonStaticValues(dirs)
	g.Expect(err).ShouldNot(HaveOccurred(), "should load common values")
	g.Expect(vals).Should(MatchAllKeys(Keys{
		"modOne": MatchAllKeys(Keys{
			"param1": Equal("val2"),
			"param2": Equal("val2"),
		}),
		"modTwo": MatchAllKeys(Keys{
			"param1": Equal("val2"),
			"param2": Equal("val2"),
		}),
		"moduleThree": MatchAllKeys(Keys{
			"param1": Equal("val3"),
		}),
	}), "should load values for mod-one and mod-two from dir2 and values for module-three from dir3")
}
