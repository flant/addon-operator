package validation_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
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

	configSchemaYaml := `
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

	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", moduleValues)
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

	configSchemaYaml := `
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
    default: { testvalue1: qweqweqw }
    properties:
      aq:
        type: string
`
	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", moduleValues)
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
      p11: testvalue1
      p12: randomvalue
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

	configSchemaYaml := `
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

	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", moduleValues)
	g.Expect(mErr.Error()).Should(ContainSubstring("3 errors occurred"))
	g.Expect(mErr.Error()).Should(ContainSubstring("forbidden property"))
}

func Test_Validate_MultiplyOfInt(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  sampling: 100
#  sampling2: 56.105
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	configSchemaYaml := `
type: object
additionalProperties: false
properties:
  sampling:
    type: number
    minimum: 0.01
    maximum: 100.0
    multipleOf: 0.01
  sampling2:
    type: number
    minimum: 0.01
    maximum: 100.0
    multipleOf: 0.01
`

	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", moduleValues)
	g.Expect(mErr).ShouldNot(HaveOccurred())
}

func Test_ValidateConfigValues_CEL(t *testing.T) {
	g := NewWithT(t)

	// Prepare module values that violate the CEL rule
	values, err := utils.NewValuesFromBytes([]byte(`
moduleName:
  replicas: 1
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Schema with a CEL validation: replicas must be > 0
	schema := `
type: object
properties:
  replicas:
    type: integer
x-deckhouse-validations:
  - expression: "self.ignoredField == 'Ignored'"
    message: "ignore not existing field"
  - expression: "self.replicas < 1"
    message: "replicas must be greater than 1"
`

	vs, err := modules.NewValuesStorage("moduleName", nil, []byte(schema), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = vs.GetSchemaStorage().ValidateConfigValues("moduleName", values)
	g.Expect(err).Should(HaveOccurred(), "expected CEL validation error")
	g.Expect(err.Error()).Should(ContainSubstring("replicas must be greater than 1"))
}
