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

	go func() {
		//nolint:revive
		for range ch {
		}
	}()

	de.UpdateStatus("l2-load-balancer", "add", true)
	de.UpdateStatus("node-local-dns", "remove", true)
	de.UpdateStatus("openstack-cloud-provider", "add", true)
	de.UpdateStatus("openstack-cloud-provider", "add", false)

	status := de.Dump()

	expectedStatus := map[string]bool{
		"l2-load-balancer":         true,
		"openstack-cloud-provider": false,
	}

	assert.Equal(t, expectedStatus, status)

	de.UpdateStatus("node-local-dns", "add", false)
	de.UpdateStatus("openstack-cloud-provider", "add", true)

	status = de.Dump()

	expectedStatus = map[string]bool{
		"l2-load-balancer":         true,
		"openstack-cloud-provider": true,
		"node-local-dns":           false,
	}

	assert.Equal(t, expectedStatus, status)

	de.UpdateStatus("l2-load-balancer", "remove", true)

	status = de.Dump()

	expectedStatus = map[string]bool{
		"openstack-cloud-provider": true,
		"node-local-dns":           false,
	}

	assert.Equal(t, expectedStatus, status)
}
