package addon_operator

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/flant/addon-operator/pkg/app"
)

func (op *AddonOperator) registerDefaultRoutes() {
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(fmt.Sprintf(`<html>
    <head><title>Addon-operator</title></head>
    <body>
    <h1>Addon-operator</h1>
    <pre>go tool pprof http://ADDON_OPERATOR_IP:%s/debug/pprof/profile</pre>
    <p>
      <a href="/discovery">show all possible routes</a>
      <a href="/metrics">prometheus metrics for built-in parameters</a>
      <a href="/metrics/hooks">prometheus metrics for user hooks</a>
      <a href="/healthz">health url</a>
      <a href="/readyz">ready url</a>
    </p>
    </body>
    </html>`, app.ListenPort)))
	})

	op.engine.APIServer.RegisterRoute(http.MethodGet, "/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})

	op.engine.APIServer.RegisterRoute(http.MethodGet, "/readyz", func(w http.ResponseWriter, request *http.Request) {
		if op.IsStartupConvergeDone() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Startup converge done.\n"))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Startup converge in progress\n"))
		}
	})

	op.engine.APIServer.RegisterRoute(http.MethodGet, "/status/converge", func(writer http.ResponseWriter, request *http.Request) {
		convergeTasks := ConvergeTasksInQueue(op.engine.TaskQueues.GetMain())

		statusLines := make([]string, 0)
		switch op.ConvergeState.firstRunPhase {
		case firstNotStarted:
			statusLines = append(statusLines, "STARTUP_CONVERGE_NOT_STARTED")
		case firstStarted:
			if convergeTasks > 0 {
				statusLines = append(statusLines, fmt.Sprintf("STARTUP_CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
			} else {
				statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
			}
		case firstDone:
			statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
			if convergeTasks > 0 {
				statusLines = append(statusLines, fmt.Sprintf("CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
			} else {
				statusLines = append(statusLines, "CONVERGE_WAIT_TASK")
			}
		}

		_, _ = writer.Write([]byte(strings.Join(statusLines, "\n") + "\n"))
	})
}
