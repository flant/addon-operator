package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
// 'remove' operation errors are not ignored.
func (p *ValuesPatch) ApplyStrict(doc []byte) ([]byte, error) {
	patch, err := p.ToJsonPatch()
	if err != nil {
		return nil, err
	}
	return patch.Apply(doc)
}

// Apply calls jsonpatch.Apply to transform an input JSON document.
//
// jsonpatch.Patch is created for each operation to ignore
// errors from 'remove' operations.
func (p *ValuesPatch) Apply(doc []byte) ([]byte, error) {
	for _, op := range p.Operations {
		patch, err := op.ToJsonPatch()
		if err != nil {
			return nil, err
		}
		newDoc, err := patch.Apply(doc)
		// Ignore errors for remove operation.
		if err != nil && op.Op == "remove" {
			errStr := err.Error()
			if strings.HasPrefix(errStr, "error in remove for path:") {
				continue
			}
			if strings.HasPrefix(errStr, "remove operation does not apply: doc is missing path") {
				continue
			}
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
	data, err := ioutil.ReadFile(filePath)
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
	operations = append(operations, newValuesPatch.Operations...)

	return []ValuesPatch{CompactPatches(operations)}
}

// CompactPatches simplifies a patches tree — one path, one operation.
func CompactPatches(operations []*ValuesPatchOperation) ValuesPatch {
	patchesTree := make(map[string][]*ValuesPatchOperation)

	for _, op := range operations {
		// remove previous operations for subpaths if got 'remove' operation for parent path
		if op.Op == "remove" {
			for subPath := range patchesTree {
				if len(op.Path) < len(subPath) && strings.HasPrefix(subPath, op.Path+"/") {
					delete(patchesTree, subPath)
				}
			}
		}

		if _, ok := patchesTree[op.Path]; !ok {
			patchesTree[op.Path] = make([]*ValuesPatchOperation, 0)
		}

		// 'add' can be squashed to only one operation
		if op.Op == "add" {
			patchesTree[op.Path] = []*ValuesPatchOperation{op}
		}

		// 'remove' is squashed to 'remove' and 'add' for future Apply calls
		if op.Op == "remove" {
			// find most recent 'add' operation
			hasPreviousAdd := false
			for _, prevOp := range patchesTree[op.Path] {
				if prevOp.Op == "add" {
					patchesTree[op.Path] = []*ValuesPatchOperation{prevOp, op}
					hasPreviousAdd = true
				}
			}

			if !hasPreviousAdd {
				// Something bad happens — a sequence contains a 'remove' operation without previous 'add' operation
				// Append virtual 'add' operation to not fail future Apply calls.
				patchesTree[op.Path] = []*ValuesPatchOperation{
					{
						Op:    "add",
						Path:  op.Path,
						Value: "guard-patch-for-successful-remove",
					},
					op,
				}
			}
		}
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

type ApplyPatchOptions string

const Strict ApplyPatchOptions = "strict"

// ApplyValuesPatch applies a set of json patch operations to the values and returns a result
func ApplyValuesPatch(values Values, valuesPatch ValuesPatch, options ...ApplyPatchOptions) (Values, bool, error) {
	var err error

	jsonDoc, err := json.Marshal(values)
	if err != nil {
		return nil, false, err
	}

	var resJsonDoc []byte
	if len(options) > 0 && options[0] == Strict {
		resJsonDoc, err = valuesPatch.ApplyStrict(jsonDoc)
	} else {
		resJsonDoc, err = valuesPatch.Apply(jsonDoc)
	}
	if err != nil {
		return nil, false, err
	}

	resValues := make(Values)
	if err = json.Unmarshal(resJsonDoc, &resValues); err != nil {
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
