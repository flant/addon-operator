package nelm

import (
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	nelmLog "github.com/werf/nelm/pkg/log"
)

func Test_NewNelmClient(t *testing.T) {
	logger := log.NewNop()
	cl := NewNelmClient(&CommonOptions{}, logger.Named("nelm"), map[string]string{})
	assert.NotNil(t, cl)
	singleLogger := nelmLog.Default

	logger2 := log.NewNop()
	_ = NewNelmClient(&CommonOptions{}, logger2.Named("test"), map[string]string{})
	assert.Equal(t, singleLogger, nelmLog.Default) // logger has not changed
}
