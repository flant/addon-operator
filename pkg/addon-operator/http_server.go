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

// registerReadyzRoute registers a readiness endpoint for the AddonOperator
func (op *AddonOperator) registerReadyzRoute() {
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/readyz", op.handleReadinessCheck)
}

// handleReadinessCheck responds with the readiness state of the operator
func (op *AddonOperator) handleReadinessCheck(w http.ResponseWriter, _ *http.Request) {
	if op.LeaderElector == nil {
		// Standard mode (no HA)
		op.reportStartupConvergeStatus(w)
		return
	}

	// Handle HA mode
	if op.LeaderElector.IsLeader() {
		// This instance is the leader - check its own readiness
		op.reportStartupConvergeStatus(w)
		return
	}

	// This instance is not the leader - check the leader's status
	op.checkLeaderReadiness(w)
}

// reportStartupConvergeStatus writes the convergence status to the response
func (op *AddonOperator) reportStartupConvergeStatus(w http.ResponseWriter) {
	if op.IsStartupConvergeDone() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Startup converge done.\n"))
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Startup converge in progress\n"))
	}
}

// checkLeaderReadiness queries the leader instance to determine overall readiness
func (op *AddonOperator) checkLeaderReadiness(w http.ResponseWriter) {
	leader := op.LeaderElector.GetLeader()
	if leader == "" {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("HA mode is enabled but no leader is elected\n"))
		return
	}

	// Create context with timeout for the request
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	// Query the leader's readiness
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s:%s/readyz", leader, app.ListenPort), nil)
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

	// If leader is OK and we're not the leader, we're in a standby state which is OK
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("HA mode is enabled and waiting for acquiring the lock.\n"))
}

// registerDefaultRoutes sets up the standard HTTP endpoints for the AddonOperator
func (op *AddonOperator) registerDefaultRoutes() {
	// Register root endpoint with HTML overview page
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/", op.handleRootPage)

	// Register health check endpoint
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/healthz", op.handleHealthCheck)

	// Register converge status endpoint
	op.engine.APIServer.RegisterRoute(http.MethodGet, "/status/converge", op.handleConvergeStatus)
}

// handleRootPage serves the main HTML page with links to all available endpoints
func (op *AddonOperator) handleRootPage(writer http.ResponseWriter, _ *http.Request) {
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
}

// handleHealthCheck responds with 200 OK for health probes
func (op *AddonOperator) handleHealthCheck(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
}

// handleConvergeStatus reports the current convergence state
func (op *AddonOperator) handleConvergeStatus(writer http.ResponseWriter, _ *http.Request) {
	convergeTasks := ConvergeTasksInQueue(op.engine.TaskQueues.GetMain())
	statusLines := generateConvergeStatusLines(op.ConvergeState.FirstRunPhase, convergeTasks)
	_, _ = writer.Write([]byte(strings.Join(statusLines, "\n") + "\n"))
}

// generateConvergeStatusLines creates status messages based on the current convergence state
func generateConvergeStatusLines(phase converge.FirstRunPhaseType, convergeTasks int) []string {
	statusLines := make([]string, 0)

	switch phase {
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

	return statusLines
}
