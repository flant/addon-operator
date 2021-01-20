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

	var configSchemaYaml = `
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
	v := NewValuesValidator()

	err = v.SchemaStorage.AddModuleValuesSchemas("moduleName", []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := v.ValidateModuleConfigValues("moduleName", moduleValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())
}

func Test_Validate_with_defaults(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  param2: {}
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	var configSchemaYaml = `
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
	v := NewValuesValidator()

	err = v.SchemaStorage.AddModuleValuesSchemas("moduleName", []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := v.ValidateModuleConfigValues("moduleName", moduleValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())
}
