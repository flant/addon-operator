package go_hook

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/flant/addon-operator/pkg/utils"
)

type PatchableValues struct {
	values          *gjson.Result
	patchOperations []*utils.ValuesPatchOperation
}

func NewPatchableValues(values map[string]interface{}) (*PatchableValues, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	res := gjson.ParseBytes(data)

	return &PatchableValues{values: &res}, nil
}

// Get value from patchable. It could be null value
func (p *PatchableValues) Get(path string) gjson.Result {
	return p.values.Get(path)
}

// GetOk returns value and `exists` flag
func (p *PatchableValues) GetOk(path string) (gjson.Result, bool) {
	v := p.values.Get(path)
	if v.Exists() {
		return v, true
	}

	return v, false
}

// GetRaw get empty interface
func (p *PatchableValues) GetRaw(path string) interface{} {
	return p.values.Get(path).Value()
}

// Exists returns is value existence flag
func (p *PatchableValues) Exists(path string) bool {
	return p.values.Get(path).Exists()
}

func (p *PatchableValues) Set(path string, value interface{}) {
	op := &utils.ValuesPatchOperation{
		Op:    "add",
		Path:  convertDotFilePathToSlashPath(path),
		Value: value,
	}

	p.patchOperations = append(p.patchOperations, op)
}

func (p *PatchableValues) Remove(path string) {
	op := &utils.ValuesPatchOperation{
		Op:   "remove",
		Path: convertDotFilePathToSlashPath(path),
	}

	p.patchOperations = append(p.patchOperations, op)
}

func (p *PatchableValues) GetPatches() []*utils.ValuesPatchOperation {
	return p.patchOperations
}

func convertDotFilePathToSlashPath(dotPath string) string {
	return strings.ReplaceAll("/"+dotPath, ".", "/")
}
