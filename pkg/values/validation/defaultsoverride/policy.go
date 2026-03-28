// Package defaultsoverride provides mechanisms for overriding default values
// in OpenAPI schemas used by addon-operator modules.
// It reads an override contract from a YAML file, validates patches
// against the contract's allowed fields, and applies them to the schema.
package defaultsoverride

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-openapi/spec"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/utils"
)

// contractsFile is the default filename for override contracts.
const contractsFile = "override.yaml"

// Contract defines the rules governing which schema fields may be overridden.
// It is loaded from a YAML file and acts as a guard: only patches targeting
// paths listed in Paths are permitted.
//
// Example YAML (override.yaml):
//
//	purpose: "cloud-provider"
//	allowedModules:
//	  - cloud-provider-aws
//	  - cloud-provider-gcp
//	paths:
//	  - network.podSubnet
//	  - network.serviceSubnet
type Contract struct {
	// Purpose describes the intent of this override contract.
	Purpose string `json:"purpose"`
	// Allowed lists the module names that may use this contract.
	Allowed []string `json:"allowed"`
	// Paths maps dot-separated property paths to the list of modules
	// that are allowed to override those fields.
	Paths []string `json:"paths"`
}

// Policy is the resolved, flattened representation of all contracts.
// It maps each property path to its access policy, used at runtime
// to decide which patches are permitted.
type Policy struct {
	// paths maps dot-separated property paths to their access policies.
	paths map[string][]string
}

// Override holds a collection of patches that override
// default values in a schema, submitted by a specific source module.
//
// Example YAML:
//
//	source: cloud-provider-aws
//	patches:
//	  - path: network.podSubnet
//	    value: "10.244.0.0/16"
//	  - path: network.serviceSubnet
//	    value: "10.96.0.0/12"
type Override struct {
	// Source is the module name requesting the overrides.
	// It is checked against PathPolicy.allowed to determine permission.
	Source string `json:"source"`
	// Source is the module name that schema should be overridden
	Target string `json:"target"`
	// Patches is an ordered list of default-value overrides to apply.
	Patches []Patch `json:"patches"`
}

// Patch defines a single default value override for a schema property.
// Path uses "." as a separator (e.g. "spec.replicas").
type Patch struct {
	// Path is the dot-separated property path within the schema.
	Path string `json:"path"`
	// Value is the new default value to set on the target property.
	Value string `json:"value"`
}

// ParseContractsFromDir reads the contracts file (override.yaml) from the given
// directory and unmarshals it into contracts.
func ParseContractsFromDir(dirPath string) ([]Contract, error) {
	raw, err := os.ReadFile(filepath.Join(dirPath, contractsFile))
	if err != nil {
		return nil, fmt.Errorf("read contracts file: %w", err)
	}

	var c []Contract
	if err = yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("unmarshal contracts file: %w", err)
	}

	return c, nil
}

// PolicyByContracts flattens the given contracts into a single Policy.
// Each contract's Allowed modules are associated with every path it declares,
// and multiple contracts contributing the same path merge their allowed lists.
func PolicyByContracts(contracts ...Contract) *Policy {
	p := &Policy{
		paths: make(map[string][]string),
	}

	for _, c := range contracts {
		for _, path := range c.Paths {
			p.paths[path] = append(p.paths[path], c.Allowed...)
		}
	}

	return p
}

// OverridesByValuesPatch extracts Override entries from a ValuesPatch.
// It scans each operation for a JSON-Patch path whose second segment is
// "override" (e.g. "/override/something") and unmarshals the operation's
// value as a list of Override structs.
func OverridesByValuesPatch(valuesPatch utils.ValuesPatch) []Override {
	var overrides []Override

	for _, op := range valuesPatch.Operations {
		pathParts := strings.Split(op.Path, "/")
		if len(pathParts) > 2 {
			if pathParts[1] == "override" {
				var override Override
				if err := yaml.Unmarshal(op.Value, &overrides); err != nil {
					continue
				}

				overrides = append(overrides, override)
			}
		}
	}

	return overrides
}

// ApplyOverride applies permitted default overrides directly to the schema.
// Each patch is checked against the policy: only patches whose paths exist
// in the policy and whose source module is in the path's allowed list are
// applied. Non-matching patches are silently skipped.
func (p *Policy) ApplyOverride(s *spec.Schema, override Override) {
	if p == nil {
		return
	}

	if len(override.Patches) == 0 || s == nil {
		return
	}

	if len(s.Properties) == 0 {
		s.Properties = make(map[string]spec.Schema)
	}

	var allowed Override
	for _, patch := range override.Patches {
		policy, ok := p.paths[patch.Path]
		if !ok {
			continue
		}

		if slices.Contains(policy, override.Source) {
			allowed.Patches = append(allowed.Patches, patch)
		}
	}

	allowed.transform(s)
}

// transform walks over every patch and applies it to the schema by
// setting default values on the matching properties.
func (t *Override) transform(s *spec.Schema) {
	if s == nil || len(t.Patches) == 0 {
		return
	}

	for _, override := range t.Patches {
		segments := strings.Split(override.Path, ".")
		setDefault(s, segments, override.Value)
	}
}

// setDefault walks the schema tree through Properties and sets
// the Default field on the leaf property.
func setDefault(s *spec.Schema, segments []string, value any) {
	if len(segments) == 0 || s == nil {
		return
	}

	propName := segments[0]
	prop, exists := s.Properties[propName]
	if !exists {
		return
	}

	if len(segments) == 1 {
		prop.Default = value
		s.Properties[propName] = prop
		return
	}

	setDefault(&prop, segments[1:], value)
	s.Properties[propName] = prop
}
