package kube_config_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Checksums(t *testing.T) {
	c := NewChecksums()

	// Adding
	c.Add("global", "qwe")

	assert.True(t, c.HasChecksum("global"))
	assert.True(t, c.HasEqualChecksum("global", "qwe"))
	assert.False(t, c.HasChecksum("non-global"), "Should be false for non added name")

	// Names
	expectedNames := map[string]struct{}{
		"global": {},
	}
	assert.Equal(t, expectedNames, c.Names())

	c.Remove("global", "unknown-checksum")
	assert.True(t, c.HasEqualChecksum("global", "qwe"))

	c.Remove("global", "qwe")
	assert.False(t, c.HasEqualChecksum("global", "qwe"))
}
