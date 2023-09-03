package validation

import (
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
	"github.com/go-openapi/validate/post"
	. "github.com/onsi/gomega"

	"github.com/flant/addon-operator/pkg/utils"
)

func Test_ApplyDefaults(t *testing.T) {
	g := NewWithT(t)

	var err error
	var moduleValues utils.Values
	moduleValues, err = utils.NewValuesFromBytes([]byte(`
moduleName:
  param1: val1
  arrayObjDefaultNoItems:
  - test1
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	configSchemaYaml := `
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
    default: testvalue1
  paramObj:
    type: object
    default:
      internal: 
        testvalue1: qweqwe
  paramObjDeep:
    type: object
    default: {}
    properties:
      param1:
        type: string
        default: randomvalue
  paramObjDeepDeep:
    default:
      deepParam1:
        param1: randomvalue
    type: object
    properties:
      deepParam1:
        type: object
        properties:
          param1:
            type: string
  arrayObjDefaultNoItems:
    default: []
    type: array
`

	v := NewValuesValidator()

	err = v.SchemaStorage.AddModuleValuesSchemas("moduleName", []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := v.ValidateModuleConfigValues("moduleName", moduleValues)

	g.Expect(mErr).ShouldNot(HaveOccurred())

	s := v.SchemaStorage.ModuleValuesSchema("moduleName", ConfigValuesSchema)

	changed := ApplyDefaults(moduleValues["moduleName"], s)

	g.Expect(changed).Should(BeTrue())
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("param2"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObj"))
	g.Expect(moduleValues["moduleName"]).Should(HaveKey("paramObjDeep"))

	q := moduleValues["moduleName"].(map[string]interface{})
	p := q["paramObj"].(map[string]interface{})
	p = p["internal"].(map[string]interface{})
	g.Expect(p).Should(HaveKey("testvalue1"))

	p = q["paramObjDeep"].(map[string]interface{})
	g.Expect(p["param1"]).Should(BeAssignableToTypeOf(""))
	str := p["param1"].(string)
	g.Expect(str).Should(Equal("randomvalue"))

	p = q["paramObjDeepDeep"].(map[string]interface{})
	p = p["deepParam1"].(map[string]interface{})
	str = p["param1"].(string)
	g.Expect(str).Should(Equal("randomvalue"))

	a := q["arrayObjDefaultNoItems"].([]interface{})
	g.Expect(a).Should(Equal([]interface{}{"test1"}))
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

	configSchemaYaml := `
type: object
additionalProperties: false
required:
- param1
properties:
  arrayObjDefaultNoItems:
    default: ["abc"]
    type: array
  param1:
    type: string
    enum:
    - val1
  param2:
    type: string
    default: testvalue1
  paramObj:
    type: object
    default:
      internal: 
        testvalue1: qweqwe
    properties:
      internal:
        type: object
        properties:
          testvalue1:
            type: string
  paramObjDeep:
    type: object
    default: {}
    properties:
      param1:
        type: string
        default: randomvalue
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
            default: randomvalue
`

	v := NewValuesValidator()

	err = v.SchemaStorage.AddModuleValuesSchemas("moduleName", []byte(configSchemaYaml), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	// mErr := v.ValidateModuleConfigValues("moduleName", moduleValues)
	// g.Expect(mErr).ShouldNot(HaveOccurred())

	s := v.SchemaStorage.ModuleValuesSchema("moduleName", ConfigValuesSchema)

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
	g.Expect(p).Should(HaveKey("testvalue1"))

	p = q["paramObjDeep"].(map[string]interface{})
	str := p["param1"].(string)
	g.Expect(str).Should(Equal("randomvalue"))

	p = q["paramObjDeepDeep"].(map[string]interface{})
	p = p["deepParam1"].(map[string]interface{})
	str = p["param1"].(string)
	g.Expect(str).Should(Equal("randomvalue"))

	a := q["arrayObjDefaultNoItems"].([]interface{})
	g.Expect(a).Should(Equal([]interface{}{"abc"}))
}
