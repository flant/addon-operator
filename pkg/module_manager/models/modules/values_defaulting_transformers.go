package modules

import (
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

type transformer interface {
	Transform(values utils.Values) utils.Values
}

type applyDefaultsForGlobal struct {
	SchemaType      validation.SchemaType
	ValuesValidator validator
}

func (a *applyDefaultsForGlobal) Transform(values utils.Values) utils.Values {
	s := a.ValuesValidator.GetSchema(validation.GlobalSchema, a.SchemaType, utils.GlobalValuesKey)
	if s == nil {
		return values
	}

	validation.ApplyDefaults(values, s)

	return values
}

type applyDefaultsForModule struct {
	ModuleName      string
	SchemaType      validation.SchemaType
	ValuesValidator validator
}

func (a *applyDefaultsForModule) Transform(values utils.Values) utils.Values {
	s := a.ValuesValidator.GetSchema(validation.ModuleSchema, a.SchemaType, utils.ModuleNameToValuesKey(a.ModuleName))
	if s == nil {
		return values
	}

	validation.ApplyDefaults(values, s)

	return values
}
