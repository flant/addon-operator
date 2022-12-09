package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/evanphx/json-patch"
)

type ValuesPatchType string

const ConfigMapPatch ValuesPatchType = "CONFIG_MAP_PATCH"
const MemoryValuesPatch ValuesPatchType = "MEMORY_VALUES_PATCH"

type ValuesPatch struct {
	Operations []*ValuesPatchOperation
}

func NewValuesPatch() *ValuesPatch {
	return &ValuesPatch{
		Operations: make([]*ValuesPatchOperation, 0),
	}
}

// ToJsonPatch returns a jsonpatch.Patch with all operations.
func (p *ValuesPatch) ToJsonPatch() (jsonpatch.Patch, error) {
	data, err := json.Marshal(p.Operations)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(data)
	if err != nil {
		return nil, err
	}
	return patch, nil
}

// ApplyStrict calls jsonpatch.Apply to transform a JSON document according to the patch.
//
// - "remove" operation errors are not ignored.
// - absent paths are not ignored.
func (p *ValuesPatch) ApplyStrict(doc []byte) ([]byte, error) {
	patch, err := p.ToJsonPatch()
	if err != nil {
		return nil, err
	}
	return patch.Apply(doc)
}

// Apply calls jsonpatch.Apply to transform an input JSON document.
//
// - errors from "remove" operations are ignored.
func (p *ValuesPatch) ApplyIgnoreNonExistentPaths(doc []byte) ([]byte, error) {
	for _, op := range p.Operations {
		patch, err := op.ToJsonPatch()
		if err != nil {
			return nil, err
		}
		newDoc, err := patch.Apply(doc)

		// Ignore errors for remove operation.
		if op.Op == "remove" && IsNonExistentPathError(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		doc = newDoc
	}

	return doc, nil
}

func (p *ValuesPatch) MergeOperations(src *ValuesPatch) {
	if src == nil {
		return
	}
	p.Operations = append(p.Operations, src.Operations...)
}

type ValuesPatchOperation struct {
	Op    string      `json:"op,omitempty"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

func (op *ValuesPatchOperation) ToString() string {
	data, err := json.Marshal(op.Value)
	if err != nil {
		// This should not happen, because ValuesPatchOperation is created with Unmarshal!
		return fmt.Sprintf("{\"op\":\"%s\", \"path\":\"%s\", \"value-error\": \"%s\" }", op.Op, op.Path, err)
	}
	return string(data)
}

// ToJsonPatch returns a jsonpatch.Patch with one operation.
func (op *ValuesPatchOperation) ToJsonPatch() (jsonpatch.Patch, error) {
	opBytes, err := json.Marshal([]*ValuesPatchOperation{op})
	if err != nil {
		return nil, err
	}
	return jsonpatch.DecodePatch(opBytes)
}

func JsonPatchFromReader(r io.Reader) (jsonpatch.Patch, error) {
	var operations = make([]jsonpatch.Operation, 0)

	dec := json.NewDecoder(r)
	for {
		var jsonStreamItem interface{}
		if err := dec.Decode(&jsonStreamItem); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		switch v := jsonStreamItem.(type) {
		case []interface{}:
			for _, item := range v {
				operation, err := DecodeJsonPatchOperation(item)
				if err != nil {
					return nil, err
				}
				operations = append(operations, operation)
			}
		case map[string]interface{}:
			operation, err := DecodeJsonPatchOperation(v)
			if err != nil {
				return nil, err
			}
			operations = append(operations, operation)
		}
	}

	return jsonpatch.Patch(operations), nil
}

func JsonPatchFromBytes(data []byte) (jsonpatch.Patch, error) {
	return JsonPatchFromReader(bytes.NewReader(data))
}
func JsonPatchFromString(in string) (jsonpatch.Patch, error) {
	return JsonPatchFromReader(strings.NewReader(in))
}

func DecodeJsonPatchOperation(v interface{}) (jsonpatch.Operation, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal operation to bytes: %s", err)
	}

	var res jsonpatch.Operation
	err = json.Unmarshal(data, &res)
	if err != nil {
		return nil, fmt.Errorf("unmarshal operation from bytes: %s", err)
	}
	return res, nil
}

// ValuesPatchFromBytes reads a JSON stream of json patches
// and single operations from bytes and returns a ValuesPatch with
// all json patch operations.
// TODO do we need a separate ValuesPatchOperation type??
func ValuesPatchFromBytes(data []byte) (*ValuesPatch, error) {
	// Get combined patch from bytes
	patch, err := JsonPatchFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("bad json-patch data: %s\n%s", err, string(data))
	}

	// Convert combined patch to bytes
	combined, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("json patch marshal: %s\n%s", err, string(data))
	}

	var operations []*ValuesPatchOperation
	if err := json.Unmarshal(combined, &operations); err != nil {
		return nil, fmt.Errorf("values patch operations: %s\n%s", err, string(data))
	}

	return &ValuesPatch{Operations: operations}, nil
}

func ValuesPatchFromFile(filePath string) (*ValuesPatch, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %s", filePath, err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	return ValuesPatchFromBytes(data)
}

func AppendValuesPatch(valuesPatches []ValuesPatch, newValuesPatch ValuesPatch) []ValuesPatch {
	return CompactValuesPatches(valuesPatches, newValuesPatch)
}

func CompactValuesPatches(valuesPatches []ValuesPatch, newValuesPatch ValuesPatch) []ValuesPatch {
	operations := []*ValuesPatchOperation{}

	for _, patch := range valuesPatches {
		operations = append(operations, patch.Operations...)
	}

	return []ValuesPatch{CompactPatches(operations, newValuesPatch.Operations)}
}

// CompactPatches modifies an array of existed patch operations according to the new array
// of patch operations. The rule is: only last operation for the path should be stored.
func CompactPatches(existedOperations []*ValuesPatchOperation, newOperations []*ValuesPatchOperation) ValuesPatch {
	patchesTree := make(map[string][]*ValuesPatchOperation)

	// Fill the map with paths from existed operations.
	for _, op := range existedOperations {
		if _, ok := patchesTree[op.Path]; !ok {
			patchesTree[op.Path] = make([]*ValuesPatchOperation, 0)
		}
		patchesTree[op.Path] = append(patchesTree[op.Path], op)
	}

	for _, op := range newOperations {
		// Remove previous operations for subpaths if there is a new operation for the parent path.
		// Subpaths removing is obvious for the "remove" operation, but not for the "add" operation.
		// This value in the "add" operation may create new subpaths and
		// other new patches may depends on these subpaths.
		// Consider this operations:
		// {"op":"app", "path":"/obj/settings", "value":{"parent_1":{}} }
		// {"op":"app", "path":"/obj/settings/parent_1/field1", "value":"foo"}
		// {"op":"app", "path":"/obj/settings/parent_1/field2", "value":"bar"}
		// The next operations from the hook may reset /obj/settings in this manner:
		// {"op":"app", "path":"/obj/settings", "value":{} }
		// The result without subpaths removing will be this:
		// {"op":"app", "path":"/obj/settings", "value":{} }
		// {"op":"app", "path":"/obj/settings/parent_1/field1", "value":"foo"}
		// {"op":"app", "path":"/obj/settings/parent_1/field2", "value":"bar"}
		// There are two problems here:
		// 1. /obj/settings/parent_1/field1 and /obj/settings/parent_1/field2
		//    were not in the latest operations from the hooks. These subpaths are not actual.
		// 2. There is no operation for "parent_1" node! Apply will fail.
		// Subpaths removing is the solution for this problems.
		if op.Op == "remove" || op.Op == "add" {
			for subPath := range patchesTree {
				if len(subPath) > len(op.Path) && strings.HasPrefix(subPath, op.Path+"/") {
					delete(patchesTree, subPath)
				}
			}
		}

		// Prepare array for the path.
		if _, ok := patchesTree[op.Path]; !ok {
			patchesTree[op.Path] = make([]*ValuesPatchOperation, 0)
		}

		// Only one last operation is stored for the same path.
		patchesTree[op.Path] = []*ValuesPatchOperation{op}
	}

	// Sort paths for proper 'add' sequence
	paths := []string{}
	for path := range patchesTree {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	newOps := []*ValuesPatchOperation{}
	for _, path := range paths {
		newOps = append(newOps, patchesTree[path]...)
	}

	newValuesPatch := ValuesPatch{Operations: newOps}
	return newValuesPatch
}

type ApplyPatchMode string

const Strict ApplyPatchMode = "strict"
const IgnoreNonExistentPaths ApplyPatchMode = "ignore-non-existent-paths"

// ApplyValuesPatch applies a set of json patch operations to the values and returns a result
func ApplyValuesPatch(values Values, valuesPatch ValuesPatch, mode ApplyPatchMode) (Values, bool, error) {
	var err error

	jsonDoc, err := json.Marshal(values)
	if err != nil {
		return nil, false, err
	}

	var resJSONDoc []byte
	switch mode {
	case Strict:
		resJSONDoc, err = valuesPatch.ApplyStrict(jsonDoc)
	case IgnoreNonExistentPaths:
		resJSONDoc, err = valuesPatch.ApplyIgnoreNonExistentPaths(jsonDoc)
	}
	if err != nil {
		return nil, false, err
	}

	resValues := make(Values)
	if err = json.Unmarshal(resJSONDoc, &resValues); err != nil {
		return nil, false, err
	}

	valuesChanged := !reflect.DeepEqual(values, resValues)

	return resValues, valuesChanged, nil
}

func ValidateHookValuesPatch(valuesPatch ValuesPatch, permittedRootKey string) error {
	for _, op := range valuesPatch.Operations {
		if op.Op == "replace" {
			return fmt.Errorf("unsupported patch operation '%s': '%s'", op.Op, op.ToString())
		}

		pathParts := strings.Split(op.Path, "/")
		if len(pathParts) > 1 {
			rootKey := pathParts[1]
			// patches for permittedRoot are allowed
			if rootKey == permittedRootKey {
				continue
			}
			// patches for *Enabled keys are accepted from global hooks
			if strings.HasSuffix(rootKey, "Enabled") && permittedRootKey == GlobalValuesKey {
				continue
			}
			// all other patches are denied
			permittedMessage := fmt.Sprintf("only '%s' accepted", permittedRootKey)
			if permittedRootKey == GlobalValuesKey {
				permittedMessage = fmt.Sprintf("only '%s' and '*Enabled' are permitted", permittedRootKey)
			}
			return fmt.Errorf("unacceptable patch operation for path '%s' (%s): '%s'", rootKey, permittedMessage, op.ToString())
		}
	}

	return nil
}

func FilterValuesPatch(valuesPatch ValuesPatch, rootPath string) ValuesPatch {
	resOps := []*ValuesPatchOperation{}

	for _, op := range valuesPatch.Operations {
		pathParts := strings.Split(op.Path, "/")
		if len(pathParts) > 1 {
			// patches for acceptableKey are allowed
			if pathParts[1] == rootPath {
				resOps = append(resOps, op)
			}
		}
	}

	newValuesPatch := ValuesPatch{Operations: resOps}
	return newValuesPatch
}

func EnabledFromValuesPatch(valuesPatch ValuesPatch) ValuesPatch {
	resOps := []*ValuesPatchOperation{}

	for _, op := range valuesPatch.Operations {
		pathParts := strings.Split(op.Path, "/")
		if len(pathParts) > 1 {
			// patches for acceptableKey are allowed
			if strings.HasSuffix(pathParts[1], "Enabled") {
				resOps = append(resOps, op)
			}
		}
	}

	newValuesPatch := ValuesPatch{Operations: resOps}
	return newValuesPatch
}

// TODO this one used only in tests
func MustValuesPatch(res *ValuesPatch, err error) *ValuesPatch {
	if err != nil {
		panic(err)
	}
	return res
}

// Error messages to distinguish non-typed errors from the 'json-patch' library.
const NonExistentPathErrorMsg = "error in remove for path:"
const MissingPathErrorMsg = "remove operation does not apply: doc is missing path"

func IsNonExistentPathError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	if strings.HasPrefix(errStr, NonExistentPathErrorMsg) {
		return true
	}
	if strings.HasPrefix(errStr, MissingPathErrorMsg) {
		return true
	}
	return false
}
