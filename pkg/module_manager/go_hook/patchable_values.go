package go_hook

import (
	"strings"

	"github.com/Jeffail/gabs"

	"github.com/flant/addon-operator/pkg/utils"
)

type PatchableValues struct {
	Values          *gabs.Container
	patchOperations []*utils.ValuesPatchOperation
}

func NewPatchableValues(values map[string]interface{}) (*PatchableValues, error) {
	gabsContainer, err := gabs.Consume(values)
	if err != nil {
		return nil, err
	}

	return &PatchableValues{Values: gabsContainer}, nil
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
