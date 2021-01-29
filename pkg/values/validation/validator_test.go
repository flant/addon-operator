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

func Test_Validate_additional_values_false(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  paramObj:
    p1:
      p11: azaza
      p12: ololo
      # deep forbidden property
      asd: qwe
    p2:
    - odin
    - two
  paramArray:
  -
    - deepParam1: a
      deepParam2: b
      deepParam3: c
  -
    - deepParam1: x
      deepParam2: "y"
      deepParam3: z
      # deep forbidden property
      aaa: zzz
    - deepParam1: j
      deepParam2: k
      deepParam3: l
  # forbidden property
  paramArrayy:
  - 1
  - 2
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	var configSchemaYaml = `
type: object
properties:
  param1:
    type: string
    enum:
    - val1
  paramObj:
    type: object
    properties:
      p1:
        type: object
        properties:
          p11:
            type: string
          p12:
            type: string
      p2:
        type: array
        items:
          type: string
  paramArray:
    type: array
    items:
      type: array
      items:
        type: object
        properties:
          deepParam1:
            type: string
          deepParam2:
            type: string
          deepParam3:
            type: string
`
	v := NewValuesValidator()

	err = v.SchemaStorage.AddModuleValuesSchemas("moduleName", []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := v.ValidateModuleConfigValues("moduleName", moduleValues)
	g.Expect(mErr.Error()).Should(ContainSubstring("3 errors occurred"))
	g.Expect(mErr.Error()).Should(ContainSubstring("forbidden property"))

}
