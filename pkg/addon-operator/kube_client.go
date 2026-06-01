package addon_operator

import (
	"context"
	"fmt"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/shell-operator/pkg/kube/dedupclient"
)

// InitDefaultHelmResourcesManager constructs the helm-resources manager on
// top of the operator's singleton deduplicated kube client.
//
// There is no separate client-go instance any more: the manager lists
// module resources through the same *dedupclient.Client shell-operator uses
// for everything else, so the whole process keeps exactly one kube client
// and one watch/cache layer.
func InitDefaultHelmResourcesManager(
	ctx context.Context,
	namespace string,
	kubeClient *dedupclient.Client,
	logger *log.Logger,
) (helm_resources_manager.HelmResourcesManager, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("kube client not set")
	}

	mgr, err := helm_resources_manager.NewHelmResourcesManager(ctx, kubeClient, logger.Named("helm-resource-manager"))
	if err != nil {
		return nil, fmt.Errorf("initialize Helm resources manager: %s\n", err)
	}

	mgr.WithDefaultNamespace(namespace)

	return mgr, nil
}
