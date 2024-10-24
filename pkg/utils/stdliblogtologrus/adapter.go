package stdliblogtologrus

import (
	"io"
	"log"
	"strings"

	"github.com/flant/shell-operator/pkg/unilogger"
)

func InitAdapter(logger *unilogger.Logger) {
	log.SetOutput(&writer{logger: logger.Named("helm")})
}

var _ io.Writer = (*writer)(nil)

type writer struct {
	logger *unilogger.Logger
}

func (w *writer) Write(msg []byte) (n int, err error) {
	// There is no loglevel for stdlib logger
	w.logger.Info(strings.TrimSpace(string(msg)))
	return 0, nil
}
