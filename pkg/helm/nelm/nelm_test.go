package nelm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	nelmLog "github.com/werf/nelm/pkg/log"

	"github.com/flant/addon-operator/pkg"
)

func Test_NewNelmClient(t *testing.T) {
	InitDefaultLogger(log.NewNop())
	singleLogger := nelmLog.Default

	cl := NewNelmClient(&CommonOptions{}, log.NewNop().Named("nelm"), map[string]string{})
	assert.NotNil(t, cl)
	assert.Equal(t, singleLogger, nelmLog.Default, "NewNelmClient must not override the global nelm logger")

	InitDefaultLogger(log.NewNop())
	assert.Equal(t, singleLogger, nelmLog.Default, "InitDefaultLogger must set the global nelm logger only once")
}

func Test_NelmLogger_ModuleFromContext(t *testing.T) {
	buf := &bytes.Buffer{}
	nl := newNelmLogger(log.NewLogger(log.WithOutput(buf)))

	nl.Info(contextWithModule(context.Background(), "node-manager"), "progress for %s", "node-manager")

	var entry map[string]any
	assert.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.Equal(t, "node-manager", entry[pkg.LogKeyModule])
	assert.Equal(t, "progress for node-manager", entry["msg"])

	buf.Reset()
	nl.Info(context.Background(), "no module here")

	var neutral map[string]any
	assert.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &neutral))
	_, hasModule := neutral[pkg.LogKeyModule]
	assert.False(t, hasModule, "module must be absent when context has no module")
}
