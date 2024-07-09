package kube_config

import (
	"context"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
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

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(moduleName string, _ map[string]string) (*bool, error) {
	return e.kubeConfigManager.IsModuleEnabled(moduleName), nil
}

func (e *Extender) IsTerminator() bool {
	return false
}

func (e *Extender) sendNotify(kubeConfigEvent config.KubeConfigEvent) {
	if e.notifyCh != nil {
		e.notifyCh <- extenders.ExtenderEvent{
			ExtenderName:      Name,
			EncapsulatedEvent: kubeConfigEvent,
		}
	}
}

func (e *Extender) SetNotifyChannel(ctx context.Context, ch chan extenders.ExtenderEvent) {
	e.notifyCh = ch
	go func() {
		for {
			select {
			case kubeConfigEvent := <-e.kubeConfigManager.KubeConfigEventCh():
				e.sendNotify(kubeConfigEvent)
			case <-ctx.Done():
				return
			}
		}
	}()
}
