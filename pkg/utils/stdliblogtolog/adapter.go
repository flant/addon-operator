package stdliblogtolog

import (
	"io"
	stdlog "log"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
)

func InitAdapter(logger *log.Logger) {
	stdlog.SetOutput(&writer{logger: logger.Named("helm")})
}

var _ io.Writer = (*writer)(nil)

type writer struct {
	logger *log.Logger
}

func (w *writer) Write(msg []byte) (n int, err error) {
	// There is no loglevel for stdlib logger
	w.logger.Info(strings.TrimSpace(string(msg)))
	return 0, nil
}
