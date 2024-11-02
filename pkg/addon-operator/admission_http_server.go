package addon_operator

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	log "github.com/deckhouse/deckhouse/pkg/log"
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
		log.Fatalf("Route %q is already registered", route)
	}

	as.routes[route] = handler
}

// start runs admission https server
func (as *AdmissionServer) start(ctx context.Context) {
	mux := http.NewServeMux()

	for route, handler := range as.routes {
		mux.Handle(route, handler)
	}

	log.Debugf("Registered admission routes: %v", as.routes)

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
			log.Fatal("admission server listen and serve tls", slog.String("error", err.Error()))
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
			log.Fatalf("Server Shutdown Failed:%+v", err)
		}
	}()
}
