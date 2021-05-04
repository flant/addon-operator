package go_hook

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/flant/addon-operator/pkg/utils"
)

type PatchableValues struct {
	Values          *gjson.Result
	patchOperations []*utils.ValuesPatchOperation
}

func NewPatchableValues(values map[string]interface{}) (*PatchableValues, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	res := gjson.ParseBytes(data)

	return &PatchableValues{Values: &res}, nil
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
