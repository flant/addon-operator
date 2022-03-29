package addon_operator

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func RegisterDefaultRoutes(op *AddonOperator) {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`<html>
    <head><title>Addon-operator</title></head>
    <body>
    <h1>Addon-operator</h1>
    <pre>go tool pprof goprofex http://ADDON_OPERATOR_IP:9115/debug/pprof/profile</pre>
    <p>
      <a href="/metrics">prometheus metrics</a>
      <a href="/healthz">health url</a>
    </p>
    </body>
    </html>`))
	})
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		helmHealthHandler := op.Helm.HealthzHandler()
		if helmHealthHandler == nil {
			writer.WriteHeader(http.StatusOK)
			return
		}
		helmHealthHandler(writer, request)
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, request *http.Request) {
		if op.IsStartupConvergeDone() {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("Startup converge done.\n"))
		} else {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("Startup converge in progress\n"))
		}
	})

	http.HandleFunc("/status/converge", func(writer http.ResponseWriter, request *http.Request) {
		convergeTasks := ConvergeTasksInQueue(op.TaskQueues.GetMain())

		statusLines := make([]string, 0)
		if op.IsStartupConvergeDone() {
			statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
			if convergeTasks > 0 {
				statusLines = append(statusLines, fmt.Sprintf("CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
			} else {
				statusLines = append(statusLines, "CONVERGE_WAIT_TASK")
			}
		} else {
			if op.ConvergeState.FirstStarted {
				if convergeTasks > 0 {
					statusLines = append(statusLines, fmt.Sprintf("STARTUP_CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
				} else {
					statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
				}
			} else {
				statusLines = append(statusLines, "STARTUP_CONVERGE_WAIT_TASKS")
			}
		}

		_, _ = writer.Write([]byte(strings.Join(statusLines, "\n") + "\n"))
	})
}
