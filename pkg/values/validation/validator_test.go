package validation

import (
	"github.com/flant/addon-operator/pkg/utils"
	"testing"

	. "github.com/onsi/gomega"
)

//func prepareConfigObj(g *WithT, input string) *VersionedUntyped {
//	vu := NewDefaultVersionedUntyped()
//	err := vu.Load([]byte(input))
//	g.Expect(err).ShouldNot(HaveOccurred())
//	return vu
//}

func Test_Validate_ModuleValues(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  param2: val2
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	var configOpenApi = `
type: object
additionalProperties: false
required:
- param1
minProperties: 2
properties:
  param1:
    type: string
    enum:
    - val1
  param2:
    type: string
`
	err = AddModuleValuesSchema("moduleName", "config", []byte(configOpenApi))

	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := ValidateModuleValues("moduleName", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())

	//func() {
	//	g.Expect(err).Should(HaveOccurred())
	//	g.Expect(err).To(BeAssignableToTypeOf(&multierror.Error{}))
	//	g.Expect(err.(*multierror.Error).Error()).Should(And(
	//		ContainSubstring("configVrsion is a forbidden property"),
	//		ContainSubstring("qwdqwd is a forbidden property"),
	//		ContainSubstring("schedule must be of type array"),
	//	))
	//},
}
