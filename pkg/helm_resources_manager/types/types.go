package types

import "github.com/flant/shell-operator/pkg/utils/manifest"

type AbsentResourcesEvent struct {
	ModuleName string
	Absent     []manifest.Manifest
}
