package addon_operator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/require"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
)

func TestReadinessCustomLeaderCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		cancel bool
		status int
		logged string
	}{
		{name: "ready", status: http.StatusOK},
		{
			name: "not ready", err: errors.New("leader is not ready"),
			status: http.StatusInternalServerError, logged: "leader is not ready",
		},
		{
			name: "request canceled", cancel: true,
			status: http.StatusInternalServerError, logged: context.Canceled.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			leader := readinessHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			})
			var logs bytes.Buffer
			op := &AddonOperator{
				LeaderElector: readinessElector(t, "standby", leader),
				Logger:        log.NewLogger(log.WithOutput(&logs), log.WithLevel(slog.LevelDebug)),
			}
			type contextKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "request value"))
			defer cancel()
			called := false
			var started time.Time
			WithLeaderReadinessCheck(func(ctx context.Context, identity string) error {
				called = true
				require.Equal(t, leader, identity)
				require.Equal(t, "request value", ctx.Value(contextKey{}))
				deadline, ok := ctx.Deadline()
				require.True(t, ok, "leader check needs a deadline")
				require.False(t, deadline.Before(started.Add(3*time.Second)))
				require.False(t, deadline.After(time.Now().Add(3*time.Second)))
				if tc.cancel {
					cancel()
					require.ErrorIs(t, ctx.Err(), context.Canceled)
					return ctx.Err()
				}
				return tc.err
			})(op)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
			started = time.Now()
			op.handleReadinessCheck(response, request)
			require.Equal(t, tc.status, response.Code)
			require.True(t, called)
			require.Zero(t, requests.Load(), "custom checks must not fall back to HTTP")
			if tc.logged == "" {
				require.Empty(t, logs.String())
				return
			}
			require.Contains(t, logs.String(), "leader readiness check failed")
			require.Contains(t, logs.String(), leader)
			require.Contains(t, logs.String(), tc.logged)
			require.Equal(t, "HA mode is enabled but couldn't check the leader's readiness\n", response.Body.String())
		})
	}
}

func TestReadinessDefaultLeaderCheck(t *testing.T) {
	for _, option := range []struct {
		name  string
		apply Option
	}{
		{name: "absent"},
		{name: "nil", apply: WithLeaderReadinessCheck(nil)},
	} {
		t.Run(option.name, func(t *testing.T) {
			for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
				t.Run(http.StatusText(status), func(t *testing.T) {
					var requests atomic.Int32
					leader := readinessHTTPServer(t, func(w http.ResponseWriter, request *http.Request) {
						requests.Add(1)
						if request.Method != http.MethodGet || request.URL.RequestURI() != "/readyz" {
							t.Errorf("unexpected leader request: %s %s", request.Method, request.URL.RequestURI())
						}
						w.WriteHeader(status)
					})
					op := &AddonOperator{LeaderElector: readinessElector(t, "standby", leader), Logger: log.NewNop()}
					if option.apply != nil {
						option.apply(op)
					}
					response := httptest.NewRecorder()
					op.handleReadinessCheck(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
					if status == http.StatusOK {
						require.Equal(t, http.StatusOK, response.Code)
						require.Equal(t, "HA mode is enabled and waiting for acquiring the lock.\n", response.Body.String())
					} else {
						require.Equal(t, http.StatusInternalServerError, response.Code)
						require.Equal(t, "HA mode is enabled but the leader's status response code isn't OK\n", response.Body.String())
					}
					require.EqualValues(t, 1, requests.Load())
				})
			}
		})
	}
}

func TestReadinessWithoutLeader(t *testing.T) {
	op := &AddonOperator{LeaderElector: readinessElector(t, "standby", ""), Logger: log.NewNop()}
	called := false
	WithLeaderReadinessCheck(func(context.Context, string) error {
		called = true
		return nil
	})(op)
	response := httptest.NewRecorder()
	op.handleReadinessCheck(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "HA mode is enabled but no leader is elected\n", response.Body.String())
	require.False(t, called)
}

func TestReadinessLocalConverge(t *testing.T) {
	for _, mode := range []string{"non-HA", "leader"} {
		t.Run(mode, func(t *testing.T) {
			for _, phase := range []converge.FirstConvergePhase{converge.FirstStarted, converge.FirstDone} {
				op := &AddonOperator{ConvergeState: converge.NewConvergeState(), Logger: log.NewNop()}
				op.ConvergeState.SetFirstRunPhase(phase)
				if mode == "leader" {
					op.LeaderElector = readinessElector(t, "self", "self")
				}
				called := false
				WithLeaderReadinessCheck(func(context.Context, string) error {
					called = true
					return errors.New("must not check a remote leader")
				})(op)
				response := httptest.NewRecorder()
				op.handleReadinessCheck(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
				if phase == converge.FirstDone {
					require.Equal(t, http.StatusOK, response.Code)
					require.Equal(t, "Startup converge done.\n", response.Body.String())
				} else {
					require.Equal(t, http.StatusInternalServerError, response.Code)
					require.Equal(t, "Startup converge in progress\n", response.Body.String())
				}
				require.False(t, called)
			}
		})
	}
}

func readinessHTTPServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	originalPort := app.ListenPort
	app.ListenPort = port
	t.Cleanup(func() { app.ListenPort = originalPort })
	return host
}

func readinessElector(t *testing.T, identity, leader string) *leaderelection.LeaderElector {
	t.Helper()
	leaseDuration := int32(60)
	renewTime := metav1.NewMicroTime(time.Now())
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "readiness", Namespace: "default"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: &leader, LeaseDurationSeconds: &leaseDuration, RenewTime: &renewTime,
		},
	}
	client := fake.NewSimpleClientset(lease)
	observed := make(chan struct{}, 1)
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: lease.ObjectMeta,
			Client:    client.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		},
		LeaseDuration: time.Minute,
		RenewDeadline: 30 * time.Second,
		RetryPeriod:   time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {},
			OnStoppedLeading: func() {},
			OnNewLeader:      func(string) { observed <- struct{}{} },
		},
	})
	require.NoError(t, err)
	if leader == "" {
		return elector
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		elector.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("leader elector did not stop")
		}
	})
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("leader elector did not observe the lease holder")
	}
	return elector
}
