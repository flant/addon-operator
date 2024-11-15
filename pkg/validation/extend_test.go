package validation_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
)

func Test_Validate_Extended(t *testing.T) {
	g := NewWithT(t)

	var err error

	configValuesYaml := `
type: object
additionalProperties: false
required:
- param1
#minProperties: 2
properties:
  param1:
    type: string
    enum:
    - val1
  param2:
    type: string
`
	valuesYaml := `
x-extend:
  schema: "config-values.yaml"
type: object
additionalProperties: false
required:
- memParam
#minProperties: 2
properties:
  memParam:
    type: string
    enum:
    - val1
`

	// TODO: static values
	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(configValuesYaml), []byte(valuesYaml))
	g.Expect(err).ShouldNot(HaveOccurred())

	var moduleValues utils.Values

	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  param2: val2
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateValues("moduleName", moduleValues)

	g.Expect(mErr).Should(HaveOccurred())

	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  memParam: val1
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr = valuesStorage.GetSchemaStorage().ValidateValues("moduleName", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())
}
