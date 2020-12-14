package types

import (
	// bindings constants and binding configs
	. "github.com/flant/shell-operator/pkg/hook/types"
)

// Additional binding types, specific to addon-operator
const (
	BeforeHelm      BindingType = "beforeHelm"
	AfterHelm       BindingType = "afterHelm"
	AfterDeleteHelm BindingType = "afterDeleteHelm"
	BeforeAll       BindingType = "beforeAll"
	AfterAll        BindingType = "afterAll"
)
