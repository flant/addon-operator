package defaults_test

import (
	"encoding/json"
	"testing"

	sdkutils "github.com/deckhouse/module-sdk/pkg/utils"
	"github.com/go-openapi/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation/defaults"
)

func TestParsePolicyByContracts(t *testing.T) {
	t.Run("single contract", func(t *testing.T) {
		c := defaults.OverrideContract{
			Purpose: "cloud-provider",
			Allowed: []string{"cloud-provider-aws"},
			Paths:   []string{"network.podSubnet", "network.serviceSubnet"},
		}

		p := defaults.BuildOverridePolicy(c)
		require.NotNil(t, p)
	})

	t.Run("no contracts produces valid policy", func(t *testing.T) {
		p := defaults.BuildOverridePolicy()
		require.NotNil(t, p)
	})
}

func TestApplyOverride(t *testing.T) {
	t.Run("applies permitted patch", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"my-module"},
			Paths:   []string{"replicas"},
		})
		s := schemaWithProps("replicas")

		p.ApplyOverride(s, defaults.Override{
			Source:  "my-module",
			Patches: []defaults.Patch{{Path: "replicas", Value: "3"}},
		})

		assert.Equal(t, "3", s.Properties["replicas"].Default)
	})

	t.Run("skips patch for disallowed source", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"allowed-module"},
			Paths:   []string{"replicas"},
		})
		s := schemaWithProps("replicas")

		p.ApplyOverride(s, defaults.Override{
			Source:  "other-module",
			Patches: []defaults.Patch{{Path: "replicas", Value: "3"}},
		})

		assert.Nil(t, s.Properties["replicas"].Default)
	})

	t.Run("skips patch for path not in policy", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"my-module"},
			Paths:   []string{"replicas"},
		})
		s := schemaWithProps("replicas", "image")

		p.ApplyOverride(s, defaults.Override{
			Source:  "my-module",
			Patches: []defaults.Patch{{Path: "image", Value: "nginx"}},
		})

		assert.Nil(t, s.Properties["image"].Default)
	})

	t.Run("no-op on empty patches", func(t *testing.T) {
		p := buildPolicy(t)
		s := &spec.Schema{}

		p.ApplyOverride(s, defaults.Override{Source: "m"})
	})

	t.Run("no-op on nil schema", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"m"},
			Paths:   []string{"replicas"},
		})

		p.ApplyOverride(nil, defaults.Override{
			Source:  "m",
			Patches: []defaults.Patch{{Path: "replicas", Value: "1"}},
		})
	})

	t.Run("mixed allowed and disallowed patches", func(t *testing.T) {
		p := buildPolicy(t,
			defaults.OverrideContract{
				Allowed: []string{"my-module"},
				Paths:   []string{"replicas"},
			},
			defaults.OverrideContract{
				Allowed: []string{"other-module"},
				Paths:   []string{"image"},
			},
		)
		s := schemaWithProps("replicas", "image")

		p.ApplyOverride(s, defaults.Override{
			Source: "my-module",
			Patches: []defaults.Patch{
				{Path: "replicas", Value: "5"},
				{Path: "image", Value: "nginx"},
			},
		})

		assert.Equal(t, "5", s.Properties["replicas"].Default)
		assert.Nil(t, s.Properties["image"].Default)
	})

	t.Run("multiple contracts merge permissions for same path", func(t *testing.T) {
		p := buildPolicy(t,
			defaults.OverrideContract{
				Allowed: []string{"module-a"},
				Paths:   []string{"replicas"},
			},
			defaults.OverrideContract{
				Allowed: []string{"module-b"},
				Paths:   []string{"replicas"},
			},
		)
		s := schemaWithProps("replicas")

		p.ApplyOverride(s, defaults.Override{
			Source:  "module-b",
			Patches: []defaults.Patch{{Path: "replicas", Value: "7"}},
		})

		assert.Equal(t, "7", s.Properties["replicas"].Default)
	})

	t.Run("applies nested property path", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"my-module"},
			Paths:   []string{"network.podSubnet"},
		})
		s := &spec.Schema{
			SchemaProps: spec.SchemaProps{
				Properties: map[string]spec.Schema{
					"network": {
						SchemaProps: spec.SchemaProps{
							Properties: map[string]spec.Schema{
								"podSubnet": {},
							},
						},
					},
				},
			},
		}

		p.ApplyOverride(s, defaults.Override{
			Source:  "my-module",
			Patches: []defaults.Patch{{Path: "network.podSubnet", Value: "10.244.0.0/16"}},
		})

		assert.Equal(t, "10.244.0.0/16", s.Properties["network"].Properties["podSubnet"].Default)
	})

	t.Run("applies deeply nested property path", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"m"},
			Paths:   []string{"a.b.c"},
		})
		s := &spec.Schema{
			SchemaProps: spec.SchemaProps{
				Properties: map[string]spec.Schema{
					"a": {
						SchemaProps: spec.SchemaProps{
							Properties: map[string]spec.Schema{
								"b": {
									SchemaProps: spec.SchemaProps{
										Properties: map[string]spec.Schema{
											"c": {},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		p.ApplyOverride(s, defaults.Override{
			Source:  "m",
			Patches: []defaults.Patch{{Path: "a.b.c", Value: "deep"}},
		})

		assert.Equal(t, "deep", s.Properties["a"].Properties["b"].Properties["c"].Default)
	})

	t.Run("skips patch when schema property does not exist", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"m"},
			Paths:   []string{"missing"},
		})
		s := schemaWithProps("replicas")

		p.ApplyOverride(s, defaults.Override{
			Source:  "m",
			Patches: []defaults.Patch{{Path: "missing", Value: "val"}},
		})

		_, exists := s.Properties["missing"]
		assert.False(t, exists)
	})

	t.Run("contract with multiple allowed modules", func(t *testing.T) {
		p := buildPolicy(t, defaults.OverrideContract{
			Allowed: []string{"module-a", "module-b"},
			Paths:   []string{"replicas"},
		})
		s := schemaWithProps("replicas")

		p.ApplyOverride(s, defaults.Override{
			Source:  "module-b",
			Patches: []defaults.Patch{{Path: "replicas", Value: "2"}},
		})

		assert.Equal(t, "2", s.Properties["replicas"].Default)
	})
}

func TestOverridesByValuesPatch(t *testing.T) {
	t.Run("extracts overrides from operations with /override/target path", func(t *testing.T) {
		patches := []defaults.Patch{
			{Path: "network.podSubnet", Value: "10.244.0.0/16"},
		}
		raw, err := json.Marshal(patches)
		require.NoError(t, err)

		vp := utils.ValuesPatch{
			Operations: []*sdkutils.ValuesPatchOperation{
				{
					Op:    "add",
					Path:  "/override/global",
					Value: raw,
				},
			},
		}

		result := defaults.GetOverridesByPatch(vp)
		require.Len(t, result, 1)
		assert.Equal(t, "cloud-provider-aws", result[0].Source)
		assert.Equal(t, "global", result[0].Target)
		require.Len(t, result[0].Patches, 1)
		assert.Equal(t, "network.podSubnet", result[0].Patches[0].Path)
		assert.Equal(t, "10.244.0.0/16", result[0].Patches[0].Value)
	})

	t.Run("skips operations without override path segment", func(t *testing.T) {
		vp := utils.ValuesPatch{
			Operations: []*sdkutils.ValuesPatchOperation{
				{
					Op:    "add",
					Path:  "/global/someKey",
					Value: json.RawMessage(`"value"`),
				},
			},
		}

		result := defaults.GetOverridesByPatch(vp)
		assert.Empty(t, result)
	})

	t.Run("skips operations with too short path", func(t *testing.T) {
		vp := utils.ValuesPatch{
			Operations: []*sdkutils.ValuesPatchOperation{
				{
					Op:    "add",
					Path:  "/override",
					Value: json.RawMessage(`[]`),
				},
			},
		}

		result := defaults.GetOverridesByPatch(vp)
		assert.Empty(t, result)
	})

	t.Run("skips operations with too long path", func(t *testing.T) {
		vp := utils.ValuesPatch{
			Operations: []*sdkutils.ValuesPatchOperation{
				{
					Op:    "add",
					Path:  "/override/target/extra",
					Value: json.RawMessage(`[]`),
				},
			},
		}

		result := defaults.GetOverridesByPatch(vp)
		assert.Empty(t, result)
	})

	t.Run("skips operations with invalid yaml value", func(t *testing.T) {
		vp := utils.ValuesPatch{
			Operations: []*sdkutils.ValuesPatchOperation{
				{
					Op:    "add",
					Path:  "/override/target",
					Value: json.RawMessage(`{invalid`),
				},
			},
		}

		result := defaults.GetOverridesByPatch(vp)
		assert.Empty(t, result)
	})

	t.Run("returns empty for empty values patch", func(t *testing.T) {
		vp := utils.ValuesPatch{}
		result := defaults.GetOverridesByPatch(vp)
		assert.Empty(t, result)
	})
}

// buildPolicy is a test helper that creates a OverridePolicy from the given contracts.
func buildPolicy(t *testing.T, contracts ...defaults.OverrideContract) *defaults.OverridePolicy {
	t.Helper()
	return defaults.BuildOverridePolicy(contracts...)
}

// schemaWithProps creates a schema with the given top-level property names.
func schemaWithProps(names ...string) *spec.Schema {
	props := make(map[string]spec.Schema, len(names))
	for _, n := range names {
		props[n] = spec.Schema{}
	}
	return &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Properties: props,
		},
	}
}
