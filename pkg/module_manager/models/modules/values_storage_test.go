package modules

import (
	"fmt"
	"testing"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

func TestSetConfigValues(t *testing.T) {
	initial := utils.Values{}
	vv := validation.NewValuesValidator()
	st := NewValuesStorage(initial, vv)

	configV := utils.Values{
		"ha": true,
		"modules": map[string]interface{}{
			"publicDomainTemplate": "%s.foo.bar",
		},
	}

	fmt.Println(st.SetNewConfigValues("global", configV))
}
