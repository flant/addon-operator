package types

import (
// bindings constants and binding configs
. "github.com/flant/shell-operator/pkg/hook/types"
)

// Additional binding types, specific to addon-operator
const (
	BeforeHelm      BindingType = "BEFORE_HELM"
	AfterHelm       BindingType = "AFTER_HELM"
	AfterDeleteHelm BindingType = "AFTER_DELETE_HELM"
	BeforeAll       BindingType = "BEFORE_ALL"
	AfterAll        BindingType = "AFTER_ALL"
)

func init() {
	// Add reverse index for additonal binding types
	ContextBindingType[BeforeHelm] = "beforeHelm"
	ContextBindingType[AfterHelm] =        "afterHelm"
	ContextBindingType[AfterDeleteHelm] =  "afterDeleteHelm"
	ContextBindingType[BeforeAll] =        "beforeAll"
	ContextBindingType[AfterAll] =         "afterAll"

}

