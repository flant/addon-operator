package modules

import (
	"github.com/go-openapi/spec"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

type transformer interface {
	Transform(values utils.Values) utils.Values
}

type applyDefaults struct {
	SchemaType validation.SchemaType
	Schemas    map[validation.SchemaType]*spec.Schema
}

func (a *applyDefaults) Transform(values utils.Values) utils.Values {
	if a.Schemas == nil {
		return values
	}

	s := a.Schemas[a.SchemaType]
	if s == nil {
		return values
	}

	validation.ApplyDefaults(values, s)

	return values
}
