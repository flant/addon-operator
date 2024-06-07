package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewNode(t *testing.T) {
	n := NewNode()
	assert.Equal(t, &Node{}, n)

	m := MockModule{
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
