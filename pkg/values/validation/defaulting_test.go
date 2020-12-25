package validation

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
	"github.com/go-openapi/validate/post"

	"github.com/flant/addon-operator/pkg/utils"
)

func Test_ApplyDefaults(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	var configOpenApi = `
type: object
additionalProperties: false
required:
- param1
properties:
  param1:
    type: string
    enum:
    - val1
  param2:
    type: string
    default: azaza
  paramObj:
    type: object
    default:
      internal: 
        azaza: qweqwe
  paramObjDeep:
    type: object
    default: {}
    properties:
      param1:
        type: string
        default: ololo
  paramObjDeepDeep:
    default:
      deepParam1:
        param1: ololo
    type: object
    properties:
      deepParam1:
        type: object
        properties:
          param1:
            type: string
`
	err = AddModuleValuesSchema("moduleName", "config", []byte(configOpenApi))

	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := ValidateModuleConfigValues("moduleName", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())

	s := GetModuleValuesSchema("moduleName", "config")

	changed := ApplyDefaults(moduleValues["moduleName"], s)

	g.Expect(changed).Should(BeTrue())
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("param2"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObj"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObjDeep"))

	q := moduleValues["moduleName"].(map[string]interface{})
	p := q["paramObj"].(map[string]interface{})
	p = p["internal"].(map[string]interface{})
	g.Expect(p).Should(HaveKey("azaza"))

	p = q["paramObjDeep"].(map[string]interface{})
	g.Expect(p["param1"]).Should(BeAssignableToTypeOf(""))
	str := p["param1"].(string)
	g.Expect(str).Should(Equal("ololo"))

	p = q["paramObjDeepDeep"].(map[string]interface{})
	p = p["deepParam1"].(map[string]interface{})
	str = p["param1"].(string)
	g.Expect(str).Should(Equal("ololo"))
}

// defaulter from go-openapi as a reference
func Test_ApplyDefaults_go_openapi(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	var configOpenApi = `
type: object
additionalProperties: false
required:
- param1
properties:
  param1:
    type: string
    enum:
    - val1
  param2:
    type: string
    default: azaza
  paramObj:
    type: object
    default:
      internal: 
        azaza: qweqwe
  paramObjDeep:
    type: object
    default: {}
    properties:
      param1:
        type: string
        default: ololo
  paramObjDeepDeep:
    type: object
    default:
      deepParam1: {}
    properties:
      deepParam1:
        type: object
        properties:
          param1:
            type: string
            default: ololo
`

	err = AddModuleValuesSchema("moduleName", "config", []byte(configOpenApi))

	g.Expect(err).ShouldNot(HaveOccurred())

	s := GetModuleValuesSchema("moduleName", "config")

	changed := ApplyDefaults(moduleValues["moduleName"], s)

	validator := validate.NewSchemaValidator(s, nil, "", strfmt.Default)

	result := validator.Validate(moduleValues["moduleName"])

	if !result.IsValid() {
		g.Expect(result.AsError()).ShouldNot(HaveOccurred())
	}

	// Add default values from openAPISpec
	post.ApplyDefaults(result)

	moduleValues["moduleName"] = result.Data().(map[string]interface{})

	g.Expect(changed).Should(BeTrue())
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("param2"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObj"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObjDeep"))

	q := moduleValues["moduleName"].(map[string]interface{})
	p := q["paramObj"].(map[string]interface{})
	p = p["internal"].(map[string]interface{})
	g.Expect(p).Should(HaveKey("azaza"))

	p = q["paramObjDeep"].(map[string]interface{})
	str := p["param1"].(string)
	g.Expect(str).Should(Equal("ololo"))

	p = q["paramObjDeepDeep"].(map[string]interface{})
	p = p["deepParam1"].(map[string]interface{})
	str = p["param1"].(string)
	g.Expect(str).Should(Equal("ololo"))

}
