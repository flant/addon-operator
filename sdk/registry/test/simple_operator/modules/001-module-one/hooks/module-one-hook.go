package hooks

import (
	gohook "github.com/flant/addon-operator/pkg/go-hook"
	"github.com/flant/addon-operator/sdk"
)

// TODO: remove global logger?
var _ = sdk.RegisterFunc(&gohook.HookConfig{}, main)

func main(_ *gohook.HookInput) error {
	return nil
}
