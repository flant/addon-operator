package types

import "github.com/flant/kube-client/manifest"

type ReleaseStatusEvent struct {
	ModuleName       string
	Absent           []manifest.Manifest
	UnexpectedStatus bool
}
