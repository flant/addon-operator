package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/values/validation"

	. "github.com/onsi/gomega"
)

func TestNewFileSystemLoader(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)

	_ = os.MkdirAll(filepath.Join(tmpDir, "modules", "001-foo-bar"), 0777)

	err := os.WriteFile(filepath.Join(tmpDir, "modules", "values.yaml"), []byte(`
fooBarEnabled: true
fooBar:
  replicas: 2
  hello:
    world: "bzzzz"
`), 0666)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "values.yaml"), []byte(`
fooBar:
  replicas: 3
  hello:
    world: "xxx"
`), 0666)
	require.NoError(t, err)

	_ = os.MkdirAll(filepath.Join(tmpDir, "modules", "001-foo-bar", "openapi"), 0777)

	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "values.yaml"), []byte(`
fooBar:
  replicas: 3
  hello:
    world: "xxx"
`), 0666)
	require.NoError(t, err)

	//	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "openapi", "config-values.yaml"), []byte(`
	//type: object
	//properties:
	//  replicas:
	//    type: integer
	//    default: 1
	//  hello:
	//    type: object
	//    default: {}
	//    description: "Pod Security Standards policy settings."
	//    properties:
	//      world:
	//        type: string
	//        default: "Hello world"
	//`), 0666)
	//	require.NoError(t, err)
	//
	//	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "openapi", "values.yaml"), []byte(`
	//x-extend:
	//  schema: config-values.yaml
	//type: object
	//properties:
	//  internal:
	//    type: object
	//    default: {}
	//`), 0666)
	//	require.NoError(t, err)

	vv := validation.NewValuesValidator()

	loader := NewFileSystemLoader(filepath.Join(tmpDir, "modules"), vv)
	modules, err := loader.LoadModules()
	require.NoError(t, err)
	m := modules[0]
	fmt.Println(m.Name)
	fmt.Println(m.GetValues(false))
}

func TestDirWithSymlinks(t *testing.T) {
	g := NewWithT(t)
	dir := "testdata/module_loader/dir1"

	ld := NewFileSystemLoader(dir, validation.NewValuesValidator())

	mods, err := ld.LoadModules()
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods).Should(HaveLen(2))

	//g.Expect(mods.Has("module-one")).Should(BeTrue())
	//g.Expect(mods.Has("module-two")).Should(BeTrue())

	//g.Expect(err).ShouldNot(HaveOccurred(), "should load common values")
	//g.Expect(vals).Should(MatchAllKeys(Keys{
	//	"moduleOne": MatchAllKeys(Keys{
	//		"param1": Equal("val1"),
	//		"param2": Equal("val2"),
	//	}),
	//	"moduleTwo": MatchAllKeys(Keys{
	//		"param1": Equal("val1"),
	//	}),
	//}), "should load values for module-one and module-two")
}

func TestLoadMultiDir(t *testing.T) {
	g := NewWithT(t)
	dirs := "testdata/module_loader/dir2:testdata/module_loader/dir3"

	ld := NewFileSystemLoader(dirs, validation.NewValuesValidator())

	mods, err := ld.LoadModules()
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods).Should(HaveLen(2))

	//g.Expect(mods.Has("module-one")).ShouldNot(BeTrue(), "should not load module-one")
	//g.Expect(mods.Has("module-two")).ShouldNot(BeTrue())
	//
	//g.Expect(mods.Has("mod-one")).Should(BeTrue(), "should load module-one as mod-one")
	//g.Expect(mods.Has("mod-two")).Should(BeTrue(), "should load module-one as mod-two")

	//vals, err := LoadCommonStaticValues(dirs)
	//g.Expect(err).ShouldNot(HaveOccurred(), "should load common values")
	//g.Expect(vals).Should(MatchAllKeys(Keys{
	//	"modOne": MatchAllKeys(Keys{
	//		"param1": Equal("val2"),
	//		"param2": Equal("val2"),
	//	}),
	//	"modTwo": MatchAllKeys(Keys{
	//		"param1": Equal("val2"),
	//		"param2": Equal("val2"),
	//	}),
	//	"moduleThree": MatchAllKeys(Keys{
	//		"param1": Equal("val3"),
	//	}),
	//}), "should load values for mod-one and mod-two from dir2 and values for module-three from dir3")
}
