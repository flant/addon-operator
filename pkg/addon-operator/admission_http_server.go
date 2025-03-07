package addon_operator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
)

type AdmissionServer struct {
	listenPort string
	certsDir   string

	routes map[string]http.Handler
}

func NewAdmissionServer(listenPort, certsDir string) *AdmissionServer {
	return &AdmissionServer{
		listenPort: listenPort,
		certsDir:   certsDir,
		routes:     make(map[string]http.Handler),
	}
}

func (as *AdmissionServer) RegisterHandler(route string, handler http.Handler) {
	if _, ok := as.routes[route]; ok {
		log.Fatal("Route is already registered",
			slog.String("route", route))
	}

	as.routes[route] = handler
}

// start runs admission https server
func (as *AdmissionServer) start(ctx context.Context) {
	mux := http.NewServeMux()

	for route, handler := range as.routes {
		mux.Handle(route, handler)
	}

	log.Debug("Registered admission routes",
		slog.String("routes", fmt.Sprintf("%v", as.routes)))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", as.listenPort),
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		cert := path.Join(as.certsDir, "tls.crt")
		key := path.Join(as.certsDir, "tls.key")
		if err := srv.ListenAndServeTLS(cert, key); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				log.Info("admission server stopped")
			} else {
				log.Fatal("admission server listen and serve tls", log.Err(err))
			}
		}
	}()

	go func() {
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer func() {
			// extra handling here
			cancel()
		}()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal("Server Shutdown Failed", log.Err(err))
		}
	}()
}
