package validation

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/utils"
)

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

	mErr := ValidateModuleConfigValues("moduleName", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())
}

func Test_Validate_with_defaults(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName1:
  param1: val1
  param2: {}
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
    type: object
    default: { azaza: qweqweqw }
    properties:
      aq:
        type: string
`
	err = AddModuleValuesSchema("moduleName1", "config", []byte(configOpenApi))

	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := ValidateModuleConfigValues("moduleName1", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())
}
