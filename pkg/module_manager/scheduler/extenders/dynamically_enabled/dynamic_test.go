package dynamically_enabled

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

func TestUpdateStatus(t *testing.T) {
	de := NewExtender()
	ch := make(chan extenders.ExtenderEvent)
	de.SetNotifyChannel(context.TODO(), ch)
	var boolNilP *bool

	go func() {
		//nolint:revive
		for range ch {
		}
	}()

	l2BalancerModule := node.ModuleMock{
		Name:  "l2-load-balancer",
		Order: 1,
	}
	dnsModule := node.ModuleMock{
		Name:  "node-local-dns",
		Order: 2,
	}
	openstackCPModule := node.ModuleMock{
		Name:  "openstack-cloud-provider",
		Order: 3,
	}

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	de.UpdateStatus("openstack-cloud-provider", "add", false)

	filterResult, _ := de.Filter(l2BalancerModule)
	assert.Equal(t, true, *filterResult)
	filterResult, _ = de.Filter(dnsModule)
	assert.Equal(t, boolNilP, filterResult)
	filterResult, _ = de.Filter(openstackCPModule)
	assert.Equal(t, false, *filterResult)

	de.UpdateStatus("node-local-dns", "add", false)
	de.UpdateStatus("openstack-cloud-provider", "add", true)

	filterResult, _ = de.Filter(l2BalancerModule)
	assert.Equal(t, true, *filterResult)
	filterResult, _ = de.Filter(dnsModule)
	assert.Equal(t, false, *filterResult)
	filterResult, _ = de.Filter(openstackCPModule)
	assert.Equal(t, true, *filterResult)

	de.UpdateStatus("l2-load-balancer", "remove", true)

	filterResult, _ = de.Filter(l2BalancerModule)
	assert.Equal(t, boolNilP, filterResult)
	filterResult, _ = de.Filter(dnsModule)
	assert.Equal(t, false, *filterResult)
	filterResult, _ = de.Filter(openstackCPModule)
	assert.Equal(t, true, *filterResult)
}
