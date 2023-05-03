package addon_operator

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/apis/v1alpha1"
)

func (op *AddonOperator) StartAdmissionServer(listenPort, certsDir string) {
	if certsDir == "" {
		return
	}

	mux := http.NewServeMux()

	mux.Handle("/validate/v1alpha1/modules", v1alpha1.ValidationHandler())

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", listenPort),
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		cert := path.Join(certsDir, "tls.crt")
		key := path.Join(certsDir, "tls.key")
		if err := srv.ListenAndServeTLS(cert, key); err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		<-op.ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer func() {
			// extra handling here
			cancel()
		}()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Server Shutdown Failed:%+v", err)
		}
	}()
}
