package kube_config

import (
	"context"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

const (
	Name extenders.ExtenderName = "KubeConfig"
)

type kubeConfigManager interface {
	IsModuleEnabled(moduleName string) *bool
	KubeConfigEventCh() chan config.KubeConfigEvent
}

type Extender struct {
	notifyCh          chan extenders.ExtenderEvent
	kubeConfigManager kubeConfigManager
}

func NewExtender(kcm kubeConfigManager) *Extender {
	e := &Extender{
		kubeConfigManager: kcm,
	}

	return e
}

func (e Extender) Dump() map[string]bool {
	return nil
}

func (e Extender) Name() extenders.ExtenderName {
	return Name
}

func (e Extender) Filter(module node.ModuleInterface) (*bool, error) {
	return e.kubeConfigManager.IsModuleEnabled(module.GetName()), nil
}

func (e Extender) Order() {
}

func (e *Extender) IsNotifier() bool {
	return true
}

func (e *Extender) SetNotifyChannel(ctx context.Context, ch chan extenders.ExtenderEvent) {
	e.notifyCh = ch
	go func() {
		for {
			select {
			case kubeConfigEvent := <-e.kubeConfigManager.KubeConfigEventCh():
				e.notifyCh <- extenders.ExtenderEvent{
					ExtenderName:      e.Name(),
					EncapsulatedEvent: kubeConfigEvent,
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
