package dynamically_enabled

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
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

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	de.UpdateStatus("openstack-cloud-provider", "add", false)
	logLabels := map[string]string{}

	filterResult, _ := de.Filter("l2-load-balancer", logLabels)
	assert.Equal(t, true, *filterResult)
	filterResult, _ = de.Filter("node-local-dns", logLabels)
	assert.Equal(t, boolNilP, filterResult)
	filterResult, _ = de.Filter("openstack-cloud-provider", logLabels)
	assert.Equal(t, false, *filterResult)

	de.UpdateStatus("node-local-dns", "add", false)
	de.UpdateStatus("openstack-cloud-provider", "add", true)

	filterResult, _ = de.Filter("l2-load-balancer", logLabels)
	assert.Equal(t, true, *filterResult)
	filterResult, _ = de.Filter("node-local-dns", logLabels)
	assert.Equal(t, false, *filterResult)
	filterResult, _ = de.Filter("openstack-cloud-provider", logLabels)
	assert.Equal(t, true, *filterResult)

	de.UpdateStatus("l2-load-balancer", "remove", true)

	filterResult, _ = de.Filter("l2-load-balancer", logLabels)
	assert.Equal(t, boolNilP, filterResult)
	filterResult, _ = de.Filter("node-local-dns", logLabels)
	assert.Equal(t, false, *filterResult)
	filterResult, _ = de.Filter("openstack-cloud-provider", logLabels)
	assert.Equal(t, true, *filterResult)
}
