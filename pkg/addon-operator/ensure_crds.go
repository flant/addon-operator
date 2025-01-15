package addon_operator

import (
	"context"

	crdinstaller "github.com/deckhouse/module-sdk/pkg/crd-installer"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
)

func (op *AddonOperator) EnsureCRDs(module *modules.BasicModule) ([]string, error) {
	// do not ensure CRDs if there are no files
	if !module.CRDExist() {
		return nil, nil
	}

	cp := crdinstaller.NewCRDsInstaller(op.KubeClient().Dynamic(), module.GetCRDFilesPaths(), crdinstaller.WithExtraLabels(op.CRDExtraLabels))
	if cp == nil {
		return nil, nil
	}
	if err := cp.Run(context.TODO()); err != nil {
		return nil, err
	}

	return cp.GetAppliedGVKs(), nil
}
