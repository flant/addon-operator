package stdliblogtologrus

import (
	"io"
	"log"

	"github.com/sirupsen/logrus"
)

func InitAdapter() {
	log.SetOutput(&writer{logger: logrus.WithField("source", "helm")})
}

var _ io.Writer = (*writer)(nil)

type writer struct {
	logger *logrus.Entry
}

func (w *writer) Write(msg []byte) (n int, err error) {
	// There is no loglevel for stdlib logger
	w.logger.Info(string(msg))
	return 0, nil
}
