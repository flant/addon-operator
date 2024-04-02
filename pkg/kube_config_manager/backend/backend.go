package backend

import (
	"context"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
)

// ConfigHandler load and saves(optional) configuration for module
type ConfigHandler interface {
	// StartInformer starts backend watcher which follows a resource, parse and send module/modules values for kube_config_manager
	StartInformer(ctx context.Context, eventC chan config.Event)

	// LoadConfig loads initial modules config before starting the informer
	LoadConfig(ctx context.Context, modulesNames ...string) (*config.KubeConfig, error)

	// DeprecatedSaveConfigValues saves patches for modules in backend (if supported), overriding the configuration
	// Deprecated: saving values in the values source is not recommended and shouldn't be used anymore
	DeprecatedSaveConfigValues(ctx context.Context, key string, values utils.Values) ( /*checksum*/ string, error)
}
