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
	tests := []struct {
		name            string
		valuesYAML      string
		schemaYAML      string
		expectError     bool
		errorSubstrings []string
	}{
		{
			name: "CEL validation fails when array length < 1",
			valuesYAML: `
moduleName:
  arr: [1]
`,
			schemaYAML: `
type: object
properties:
  arr:
    type: array
x-deckhouse-validations:
  - expression: "self.arr.size() < 1"
    message: "arr must be greater than 1"
`,
			expectError:     true,
			errorSubstrings: []string{"arr must be greater than 1"},
		},
		{
			name: "CEL validation fails when array length < 1 self is array",
			valuesYAML: `
moduleName:
  arr: [1]
`,
			schemaYAML: `
type: object
properties:
  arr:
    type: array
    x-deckhouse-validations:
      - expression: "self.size() < 1"
        message: "arr must be greater than 1"
`,
			expectError:     true,
			errorSubstrings: []string{"arr must be greater than 1"},
		},
		{
			name: "CEL validation fails when replicas < 1 and self is not a map",
			valuesYAML: `
moduleName:
  replicas: 1
`,
			schemaYAML: `
type: object
properties:
  replicas:
    type: integer
    x-deckhouse-validations:
      - expression: "self < 1"
        message: "replicas must be greater than 1"
`,
			expectError:     true,
			errorSubstrings: []string{"replicas must be greater than 1"},
		},
		{
			name: "CEL validation fails when replicas < 1 and ignores not existing field",
			valuesYAML: `
moduleName:
  replicas: 1
`,
			schemaYAML: `
type: object
properties:
  replicas:
    type: integer
x-deckhouse-validations:
  - expression: "self.ignoredField == 'Ignored'"
    message: "ignore not existing field"
  - expression: "self.replicas < 1"
    message: "replicas must be greater than 1"
`,
			expectError:     true,
			errorSubstrings: []string{"replicas must be greater than 1"},
		},
		{
			name: "CEL validation works",
			valuesYAML: `
moduleName:
  a:
    b: abd
`,
			schemaYAML: `
type: object
properties:
  a:
    type: object
    properties:
      b:
        type: string

x-deckhouse-validations:
- expression: 'self.a.b == "abc"'
  message: "Not equal to abc"
`,
			expectError:     true,
			errorSubstrings: []string{"Not equal to abc"},
		},
		{
			name: "CEL validation works with nested properties",
			valuesYAML: `
moduleName:
  a:
    b: abd
    c: 122
`,
			schemaYAML: `
type: object
properties:
  a:
    type: object
    properties:
      b:
        type: string
      c:
        type: number
    x-deckhouse-validations:
      - expression: 'self.b == "abc"'
        message: "Not equal to abc"
      - expression: 'self.c == 123'
        message: "Not equal to 123"
`,
			expectError:     true,
			errorSubstrings: []string{"Not equal to abc", "Not equal to 123"},
		},
		{
			name: "CEL validation works with deep nested properties",
			valuesYAML: `
moduleName:
  a:
    b:
      c:
        d: 122
`,
			schemaYAML: `
type: object
properties:
  a:
    type: object
    properties:
      b:
        type: object
        properties:
          c:
            type: object
            properties:
              d:
                type: number
            x-deckhouse-validations:
            - expression: 'self.d == 123'
              message: "Not equal to 123"
`,
			expectError:     true,
			errorSubstrings: []string{"Not equal to 123"},
		},
		{
			name: "CEL validation works with several similliar field with different types on different levels",
			valuesYAML: `
moduleName:
  a:
    a: "ab"
    c:
      a: 21
`,
			schemaYAML: `
# several similliar field with different types on different levels
type: object
properties:
  a:
    type: object
    properties:
      a:
        type: string
      c:
        type: object
        properties:
          a:
            type: number
        x-deckhouse-validations:
        - expression: 'self.a == 25'
          message: "Not equal to 25"
    x-deckhouse-validations:
      - expression: 'self.a == "abc"'
        message: "Not equal to abc"
`,
			expectError:     true,
			errorSubstrings: []string{"Not equal to abc", "Not equal to 25"},
		},
		{
			name: "CEL validation for map values",
			valuesYAML: `
moduleName:
  mymap:
    foo: 1
    bar: 0
`,
			schemaYAML: `
type: object
properties:
  mymap:
    type: object
    foo:
      type: integer
    bar:
      type: integer
x-deckhouse-validations:
- expression: "self.mymap.all(key, self.mymap[key] > 0)"
  message: "all map values must be greater than 0"
`,
			expectError:     true,
			errorSubstrings: []string{"all map values must be greater than 0"},
		},
		{
			name: "CEL validation for additionalProperties (map values)",
			valuesYAML: `
moduleName:
  mymap:
    foo: 1
    bar: 0
`,
			schemaYAML: `
type: object
properties:
  mymap:
    type: object
    additionalProperties:
      type: integer
    x-deckhouse-validations:
    - expression: "self.all(key, self[key] > 0)"
      message: "all map values must be greater than 0"
`,
			expectError:     true,
			errorSubstrings: []string{"all map values must be greater than 0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			values, err := utils.NewValuesFromBytes([]byte(tt.valuesYAML))
			g.Expect(err).ShouldNot(HaveOccurred())

			vs, err := modules.NewValuesStorage("moduleName", nil, []byte(tt.schemaYAML), nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			err = vs.GetSchemaStorage().ValidateConfigValues("moduleName", values)
			if tt.expectError {
				g.Expect(err).Should(HaveOccurred(), "expected CEL validation error")
				for _, substr := range tt.errorSubstrings {
					g.Expect(err.Error()).Should(ContainSubstring(substr))
				}
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}
		})
	}
}
