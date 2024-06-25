package modules

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/utils"
)

func TestNewGlobalModule(t *testing.T) {
	g := NewWithT(t)
	configValues, err := utils.NewValuesFromBytes([]byte(""))
	g.Expect(err).ShouldNot(HaveOccurred())

	dep := hooks.HookExecutionDependencyContainer{}
	// empty values provided
	gm, err := NewGlobalModule("/tmp/hooks", configValues, &dep, []byte(""), []byte(""))
	g.Expect(err).ShouldNot(HaveOccurred())

	globalValues, err := utils.NewValuesFromBytes([]byte(`
enabledModules:
- moduleA
- moduleB
`))
	g.Expect(err).ShouldNot(HaveOccurred())
	mErr := gm.valuesStorage.GetSchemaStorage().ValidateValues("", globalValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())

	// no enabledModules provided
	gm, err = NewGlobalModule("/tmp/hooks", configValues, &dep, []byte(""), []byte(`
type: object
properties:
  hostname:
    type: string`))
	g.Expect(err).ShouldNot(HaveOccurred())

	globalValues, err = utils.NewValuesFromBytes([]byte(`
enabledModules:
- moduleA
- moduleB
`))
	g.Expect(err).ShouldNot(HaveOccurred())
	mErr = gm.valuesStorage.GetSchemaStorage().ValidateValues("", globalValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())

	// enabledModules of wrong type provided
	gm, err = NewGlobalModule("/tmp/hooks", configValues, &dep, []byte(""), []byte(`
type: object
properties:
  hostname:
    type: string
  enabledModules:
    type: string`))
	g.Expect(err).ShouldNot(HaveOccurred())

	globalValues, err = utils.NewValuesFromBytes([]byte(`
enabledModules:
- moduleA
- moduleB
hostname: localhost
`))
	g.Expect(err).ShouldNot(HaveOccurred())
	mErr = gm.valuesStorage.GetSchemaStorage().ValidateValues("", globalValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())

	// enabledModules provided
	gm, err = NewGlobalModule("/tmp/hooks", configValues, &dep, []byte(""), []byte(`
type: object
properties:
  hostname:
    type: string
  enabledModules:
    type: array
    items:
      type: string`))
	g.Expect(err).ShouldNot(HaveOccurred())

	globalValues, err = utils.NewValuesFromBytes([]byte(`
enabledModules:
- moduleA
- moduleB
`))
	g.Expect(err).ShouldNot(HaveOccurred())
	mErr = gm.valuesStorage.GetSchemaStorage().ValidateValues("", globalValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())

	// some wrong value provided
	gm, err = NewGlobalModule("/tmp/hooks", configValues, &dep, []byte(""), []byte(`
type: object
properties:
  enabledModules:
    type: array
    items:
      type: string
  hostname:
    type: string`))
	g.Expect(err).ShouldNot(HaveOccurred())

	globalValues, err = utils.NewValuesFromBytes([]byte(`
hostName: localhost
enabledModules:
- moduleA
- moduleB
`))
	g.Expect(err).ShouldNot(HaveOccurred())
	mErr = gm.valuesStorage.GetSchemaStorage().ValidateValues("", globalValues)
	g.Expect(mErr).Should(HaveOccurred())
}
