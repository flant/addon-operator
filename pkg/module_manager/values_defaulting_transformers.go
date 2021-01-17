package module_manager

import (
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

type ApplyDefaultsForGlobal struct {
	SchemaType      validation.SchemaType
	ValuesValidator *validation.ValuesValidator
}

func (a *ApplyDefaultsForGlobal) Transform(values utils.Values) utils.Values {
	s := a.ValuesValidator.SchemaStorage.GlobalValuesSchema(a.SchemaType)
	if s == nil {
		return values
	}
	if values.HasGlobal() {
		validation.ApplyDefaults(values[utils.GlobalValuesKey], s)
	}
	return values
}

type ApplyDefaultsForModule struct {
	ModuleValuesKey string
	SchemaType      validation.SchemaType
	ValuesValidator *validation.ValuesValidator
}

func (a *ApplyDefaultsForModule) Transform(values utils.Values) utils.Values {
	s := a.ValuesValidator.SchemaStorage.ModuleValuesSchema(a.ModuleValuesKey, a.SchemaType)
	if s == nil {
		return values
	}
	if values.HasKey(a.ModuleValuesKey) {
		validation.ApplyDefaults(values[a.ModuleValuesKey], s)
	}
	return values
}
