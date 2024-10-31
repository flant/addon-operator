package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/go_lib/log"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileSystemLoader(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)

	_ = os.MkdirAll(filepath.Join(tmpDir, "modules", "001-foo-bar"), 0o777)

	err := os.WriteFile(filepath.Join(tmpDir, "modules", "values.yaml"), []byte(`
fooBarEnabled: true
fooBar:
  replicas: 2
  hello:
    world: "bzzzz"
`), 0o666)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "values.yaml"), []byte(`
fooBar:
  replicas: 3
  hello:
    world: "xxx"
`), 0o666)
	require.NoError(t, err)

	_ = os.MkdirAll(filepath.Join(tmpDir, "modules", "001-foo-bar", "openapi"), 0o777)

	err = os.WriteFile(filepath.Join(tmpDir, "modules", "001-foo-bar", "values.yaml"), []byte(`
fooBar:
  replicas: 3
  hello:
    world: "xxx"
`), 0o666)
	require.NoError(t, err)

	loader := NewFileSystemLoader(filepath.Join(tmpDir, "modules"), log.NewNop())
	modules, err := loader.LoadModules()
	require.NoError(t, err)
	m := modules[0]
	assert.Equal(t, "foo-bar", m.Name)
	assert.YAMLEq(t, `
hello:
    world: xxx
replicas: 3
`, m.GetValues(false).AsString("yaml"))
}

func TestDirWithSymlinks(t *testing.T) {
	g := NewWithT(t)
	dir := "testdata/module_loader/dir1"

	ld := NewFileSystemLoader(dir, log.NewNop())

	mods, err := ld.LoadModules()
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods).Should(HaveLen(2))
}

func TestLoadMultiDir(t *testing.T) {
	g := NewWithT(t)
	dirs := "testdata/module_loader/dir2:testdata/module_loader/dir3"

	ld := NewFileSystemLoader(dirs, log.NewNop())

	mods, err := ld.LoadModules()
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(mods).Should(HaveLen(2))
}
