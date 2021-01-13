package module_manager

import (
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

type ApplyDefaultsForGlobal struct {
	SchemaType validation.SchemaType
}

func (a *ApplyDefaultsForGlobal) Transform(values utils.Values) utils.Values {
	s := validation.GetGlobalValuesSchema(a.SchemaType)
	if s == nil {
		return values
	}
	validation.ApplyDefaults(values, s)
	return values
}

type ApplyDefaultsForModule struct {
	ModuleName string
	SchemaType validation.SchemaType
}

func (a *ApplyDefaultsForModule) Transform(values utils.Values) utils.Values {
	s := validation.GetModuleValuesSchema(a.ModuleName, a.SchemaType)
	if s == nil {
		return values
	}
	validation.ApplyDefaults(values, s)
	return values
}
