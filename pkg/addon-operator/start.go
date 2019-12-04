package addon_operator

import (
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
)

// Start is a start command. It is here to work with import.
func Start() {
	var err error

	err = InitHttpServer(app.ListenAddress, app.ListenPort)
	if err != nil {
		log.Errorf("HTTP SERVER start failed: %v", err)
		os.Exit(1)
	}

	err = Init()
	if err != nil {
		log.Errorf("INIT failed: %v", err)
		os.Exit(1)
	}

	Run()
}
