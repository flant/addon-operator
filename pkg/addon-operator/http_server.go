package addon_operator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
)

func (op *AddonOperator) registerReadyzRoute() {
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/readyz", func(w http.ResponseWriter, request *http.Request) {
		// check if ha mode is enabled and current instance isn't the leader - return ok so as not to spam with failed readiness probes
		if op.LeaderElector != nil {
			if op.LeaderElector.IsLeader() {
				if op.IsStartupConvergeDone() {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Startup converge done.\n"))
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Startup converge in progress\n"))
				}
			} else if leader := op.LeaderElector.GetLeader(); len(leader) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%s/readyz", leader, app.ListenPort), nil)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("HA mode is enabled but couldn't craft a request to the leader\n"))
					return
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("HA mode is enabled but couldn't send a request to the leader\n"))
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("HA mode is enabled but the leader's status response code isn't OK\n"))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("HA mode is enabled and waiting for acquiring the lock.\n"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("HA mode is enabled but something went wrong\n"))
			}
		} else {
			if op.IsStartupConvergeDone() {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Startup converge done.\n"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Startup converge in progress\n"))
			}
		}
	})
}

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

	op.engine.APIServer.RegisterRoute(http.MethodGet, "/status/converge", func(writer http.ResponseWriter, request *http.Request) {
		convergeTasks := ConvergeTasksInQueue(op.engine.TaskQueues.GetMain())

		statusLines := make([]string, 0)
		switch op.ConvergeState.FirstRunPhase {
		case converge.FirstNotStarted:
			statusLines = append(statusLines, "STARTUP_CONVERGE_NOT_STARTED")
		case converge.FirstStarted:
			if convergeTasks > 0 {
				statusLines = append(statusLines, fmt.Sprintf("STARTUP_CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
			} else {
				statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
			}
		case converge.FirstDone:
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
