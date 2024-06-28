package node

import (
	"testing"

	"github.com/stretchr/testify/assert"

	node_mock "github.com/flant/addon-operator/pkg/module_manager/scheduler/node/mock"
)

func TestNewNode(t *testing.T) {
	n := NewNode()
	assert.Equal(t, &Node{}, n)

	m := node_mock.MockModule{
		Name:  "test-node",
		Order: 32,
	}

	n = NewNode().WithName("test-node").WithWeight(uint32(32)).WithType(ModuleType).WithModule(m)
	assert.Equal(t, &Node{
		name:   "test-node",
		weight: 32,
		typ:    ModuleType,
		module: m,
	}, n)
}
