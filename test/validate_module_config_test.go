package test

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
)

// parseValuesWithYAMLv3 parses YAML using gopkg.in/yaml.v3, which preserves
// integer types (unlike sigs.k8s.io/yaml which converts everything through JSON,
// turning all numbers into float64).
//
// This matches the production code path in the ConfigMap backend
// (pkg/kube_config_manager/backend/configmap/configmap.go) which uses yaml.v3.
//
// The difference:
//   - yaml.v3:          `sampling: 100` -> Go int (triggers multipleOf bug)
//   - sigs.k8s.io/yaml: `sampling: 100` -> Go float64 (masks the bug)
func parseValuesWithYAMLv3(yamlData string) (utils.Values, error) {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlData), &parsed); err != nil {
		return nil, err
	}
	return utils.Values(parsed), nil
}

// Test_Validate_RealModuleConfig_MultipleOf_YAMLv3 reproduces the production error:
//
//	"factor MultipleOf declared for istio.tracing.sampling must be positive: 0"
//
// Root cause: when YAML value `sampling: 100` is parsed by gopkg.in/yaml.v3,
// it is stored as Go int. The go-openapi/validate library's MultipleOfNativeType
// function then calls MultipleOfInt(path, in, value, int64(multipleOf)), where
// int64(0.01) truncates to 0, triggering "must be positive" error.
//
// The tests use yaml.v3 to parse values (matching the production ConfigMap backend path)
// instead of sigs.k8s.io/yaml (used by utils.NewValuesFromBytes which converts all
// numbers to float64 and masks the bug).
func Test_Validate_RealModuleConfig_MultipleOf_YAMLv3(t *testing.T) {
	configSchemaBytes, err := os.ReadFile("config-values.yaml")
	if err != nil {
		t.Fatalf("failed to read config-values.yaml: %v", err)
	}

	tests := []struct {
		name        string
		valuesYAML  string
		expectError bool
		errorSubstr string
	}{
		{
			name: "sampling as integer 100 triggers multipleOf bug",
			valuesYAML: `
istio:
  tracing:
    sampling: 100
`,
			// yaml.v3 parses 100 as int → int64(0.01)=0 → "must be positive: 0"
			expectError: true,
			errorSubstr: "must be positive",
		},
		{
			name: "sampling as float 100.0 passes validation",
			valuesYAML: `
istio:
  tracing:
    sampling: 100.0
`,
			expectError: false,
		},
		{
			name: "sampling as float 50.05 passes validation",
			valuesYAML: `
istio:
  tracing:
    sampling: 50.05
`,
			expectError: false,
		},
		{
			name: "sampling as float 0.01 (minimum) passes validation",
			valuesYAML: `
istio:
  tracing:
    sampling: 0.01
`,
			expectError: false,
		},
		{
			name: "sampling as integer 1 triggers multipleOf bug",
			valuesYAML: `
istio:
  tracing:
    sampling: 1
`,
			// yaml.v3 parses 1 as int → int64(0.01)=0 → "must be positive: 0"
			expectError: true,
			errorSubstr: "must be positive",
		},
		{
			name: "sampling as float 1.0 passes validation",
			valuesYAML: `
istio:
  tracing:
    sampling: 1.0
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			moduleValues, err := parseValuesWithYAMLv3(tt.valuesYAML)
			g.Expect(err).ShouldNot(HaveOccurred())

			valuesStorage, err := modules.NewValuesStorage("istio", nil, configSchemaBytes, nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("istio", moduleValues)
			if tt.expectError {
				g.Expect(mErr).Should(HaveOccurred(), "expected validation error for: %s", tt.name)
				g.Expect(mErr.Error()).Should(ContainSubstring(tt.errorSubstr))
			} else {
				g.Expect(mErr).ShouldNot(HaveOccurred(), "unexpected validation error: %v", mErr)
			}
		})
	}
}

// Test_Validate_RealModuleConfig_FullSettings_YAMLv3 validates the exact production
// ModuleConfig settings against the istio OpenAPI schema using yaml.v3 parsing.
// This reproduces the exact production scenario that causes a fatal startup error.
func Test_Validate_RealModuleConfig_FullSettings_YAMLv3(t *testing.T) {
	g := NewWithT(t)

	configSchemaBytes, err := os.ReadFile("config-values.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	// These settings come from the production ModuleConfig resource.
	// The key problematic field is `sampling: 100` (integer).
	valuesYAML := `
istio:
  additionalVersions:
  - "1.25"
  alliance:
    ingressGateway:
      inlet: LoadBalancer
      nodeSelector:
        node-role/loadbalancer: ""
      tolerations:
      - operator: Exists
  dataPlane:
    trafficRedirectionSetupMode: CNIPlugin
  federation:
    enabled: true
  globalVersion: "1.25"
  multicluster:
    enabled: true
  sidecar:
    resourcesManagement:
      mode: Static
      static:
        limits:
          cpu: "10"
          memory: 10Gi
        requests:
          cpu: 100m
          memory: 128Mi
  tracing:
    collector:
      zipkin:
        address: jaeger-collector.jaeger.svc.customer.p.mesh:9411
    enabled: true
    kiali:
      jaegerGRPCEndpoint: http://jaeger-query.jaeger.svc.customer.p.mesh:16685
      jaegerURLForUsers: https://jaeger-customer.apps.lmru.tech/
    sampling: 100
`

	moduleValues, err := parseValuesWithYAMLv3(valuesYAML)
	g.Expect(err).ShouldNot(HaveOccurred())

	valuesStorage, err := modules.NewValuesStorage("istio", nil, configSchemaBytes, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("istio", moduleValues)
	g.Expect(mErr).Should(HaveOccurred(), "expected the multipleOf bug to trigger with yaml.v3 parsed integer value")
	g.Expect(mErr.Error()).Should(ContainSubstring("must be positive"))
}

// Test_Validate_MultipleOf_SmallSchema_YAMLv3 tests the multipleOf bug with a minimal
// schema using yaml.v3 parsing to isolate the integer type behavior.
func Test_Validate_MultipleOf_SmallSchema_YAMLv3(t *testing.T) {
	tests := []struct {
		name        string
		valuesYAML  string
		schemaYAML  string
		expectError bool
		errorSubstr string
	}{
		{
			name: "integer value with fractional multipleOf fails (yaml.v3 preserves int type)",
			valuesYAML: `
moduleName:
  sampling: 100
`,
			schemaYAML: `
type: object
properties:
  sampling:
    type: number
    minimum: 0.01
    maximum: 100.0
    multipleOf: 0.01
`,
			// yaml.v3: 100 → int → int64(0.01)=0 → "must be positive"
			expectError: true,
			errorSubstr: "must be positive",
		},
		{
			name: "float value with fractional multipleOf passes",
			valuesYAML: `
moduleName:
  sampling: 100.0
`,
			schemaYAML: `
type: object
properties:
  sampling:
    type: number
    minimum: 0.01
    maximum: 100.0
    multipleOf: 0.01
`,
			expectError: false,
		},
		{
			name: "integer value with integer multipleOf passes",
			valuesYAML: `
moduleName:
  count: 10
`,
			schemaYAML: `
type: object
properties:
  count:
    type: integer
    multipleOf: 5
`,
			expectError: false,
		},
		{
			name: "integer value not multiple of integer multipleOf fails",
			valuesYAML: `
moduleName:
  count: 7
`,
			schemaYAML: `
type: object
properties:
  count:
    type: integer
    multipleOf: 5
`,
			expectError: true,
			errorSubstr: "should be a multiple of 5",
		},
		{
			name: "integer value with multipleOf 0.1 fails (truncation bug)",
			valuesYAML: `
moduleName:
  value: 5
`,
			schemaYAML: `
type: object
properties:
  value:
    type: number
    multipleOf: 0.1
`,
			// yaml.v3: 5 → int → int64(0.1)=0 → "must be positive"
			expectError: true,
			errorSubstr: "must be positive",
		},
		{
			name: "float value with multipleOf 0.1 passes",
			valuesYAML: `
moduleName:
  value: 5.0
`,
			schemaYAML: `
type: object
properties:
  value:
    type: number
    multipleOf: 0.1
`,
			expectError: false,
		},
		{
			name: "integer 0 value with fractional multipleOf fails",
			valuesYAML: `
moduleName:
  sampling: 0
`,
			schemaYAML: `
type: object
properties:
  sampling:
    type: number
    minimum: 0
    maximum: 100.0
    multipleOf: 0.01
`,
			// yaml.v3: 0 → int → int64(0.01)=0 → "must be positive"
			expectError: true,
			errorSubstr: "must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			moduleValues, err := parseValuesWithYAMLv3(tt.valuesYAML)
			g.Expect(err).ShouldNot(HaveOccurred())

			valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(tt.schemaYAML), nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", moduleValues)
			if tt.expectError {
				g.Expect(mErr).Should(HaveOccurred(), "expected validation error for: %s", tt.name)
				g.Expect(mErr.Error()).Should(ContainSubstring(tt.errorSubstr))
			} else {
				g.Expect(mErr).ShouldNot(HaveOccurred(), "unexpected validation error: %v", mErr)
			}
		})
	}
}

// Test_Validate_MultipleOf_ParsingDifference demonstrates the root cause: the difference
// between yaml.v3 and sigs.k8s.io/yaml when parsing integer values.
// yaml.v3 preserves int types, sigs.k8s.io/yaml converts all numbers to float64.
func Test_Validate_MultipleOf_ParsingDifference(t *testing.T) {
	g := NewWithT(t)

	schemaYAML := `
type: object
properties:
  sampling:
    type: number
    minimum: 0.01
    maximum: 100.0
    multipleOf: 0.01
`
	valuesStorage, err := modules.NewValuesStorage("moduleName", nil, []byte(schemaYAML), nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test 1: yaml.v3 parsing (production path) — sampling: 100 is parsed as int
	yamlV3Values, err := parseValuesWithYAMLv3("moduleName:\n  sampling: 100\n")
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr := valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", yamlV3Values)
	g.Expect(mErr).Should(HaveOccurred(), "yaml.v3 parsed int value should trigger multipleOf bug")
	g.Expect(mErr.Error()).Should(ContainSubstring("must be positive"))

	// Test 2: sigs.k8s.io/yaml parsing (test helper path) — sampling: 100 is parsed as float64
	sigsYamlValues, err := utils.NewValuesFromBytes([]byte("moduleName:\n  sampling: 100\n"))
	g.Expect(err).ShouldNot(HaveOccurred())

	mErr = valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", sigsYamlValues)
	g.Expect(mErr).ShouldNot(HaveOccurred(), "sigs.k8s.io/yaml parsed float64 value should pass validation")

	// Test 3: Manually constructed int value — directly prove the type matters
	intValues := utils.Values{
		"moduleName": map[string]interface{}{
			"sampling": int(100),
		},
	}
	mErr = valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", intValues)
	g.Expect(mErr).Should(HaveOccurred(), "manually constructed int value should trigger multipleOf bug")
	g.Expect(mErr.Error()).Should(ContainSubstring("must be positive"))

	// Test 4: Manually constructed float64 value — passes fine
	floatValues := utils.Values{
		"moduleName": map[string]interface{}{
			"sampling": float64(100),
		},
	}
	mErr = valuesStorage.GetSchemaStorage().ValidateConfigValues("moduleName", floatValues)
	g.Expect(mErr).ShouldNot(HaveOccurred(), "manually constructed float64 value should pass validation")
}
