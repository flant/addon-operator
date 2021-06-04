package types

import "github.com/flant/kube-client/manifest"

type AbsentResourcesEvent struct {
	ModuleName string
	Absent     []manifest.Manifest
}
