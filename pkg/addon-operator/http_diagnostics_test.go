package addon_operator

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

func TestDiagnosticRootLinks(t *testing.T) {
	serverURL := startDiagnosticAPIServer(t, true)
	for _, prefix := range []string{"", "/controller"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			baseURL := diagnosticProxyURL(t, serverURL, prefix)
			response, err := http.Get(baseURL + "/")
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "go tool pprof profile.pprof")
			require.NotContains(t, string(body), "ADDON_OPERATOR_IP")

			links := regexp.MustCompile(`href="([^"]+)"`).FindAllSubmatch(body, -1)
			var paths []string
			for _, link := range links {
				reference, err := url.Parse(string(link[1]))
				require.NoError(t, err)
				target := response.Request.URL.ResolveReference(reference)
				require.Equal(t, response.Request.URL.Host, target.Host)
				paths = append(paths, target.Path)
			}
			require.ElementsMatch(t, []string{
				prefix + "/debug/pprof/", prefix + "/discovery", prefix + "/metrics",
				prefix + "/metrics/hooks", prefix + "/healthz", prefix + "/readyz",
			}, paths)
		})
	}
}

func TestDiagnosticProfilerRoutes(t *testing.T) {
	// Standby replicas start the API server without the leader-only default routes.
	serverURL := startDiagnosticAPIServer(t, false)
	for _, prefix := range []string{"", "/controller"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			baseURL := diagnosticProxyURL(t, serverURL, prefix)
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}}
			for _, route := range []struct {
				path     string
				location string
			}{
				{path: "/debug", location: "debug/pprof/"},
				{path: "/debug/", location: "pprof/"},
				{path: "/debug/pprof", location: "pprof/"},
			} {
				for _, query := range []string{"", "?debug=1&label=a%2Fb"} {
					t.Run(route.path+query, func(t *testing.T) {
						response, err := client.Get(baseURL + route.path + query)
						require.NoError(t, err)
						defer response.Body.Close()
						require.Equal(t, http.StatusMovedPermanently, response.StatusCode)
						require.Equal(t, route.location+query, response.Header.Get("Location"))
						target, err := response.Location()
						require.NoError(t, err)
						require.Equal(t, baseURL+"/debug/pprof/"+query, target.String())
					})
				}
			}

			t.Run("canonical index", func(t *testing.T) {
				response, err := client.Get(baseURL + "/debug/pprof/")
				require.NoError(t, err)
				defer response.Body.Close()
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, response.Header.Get("Location"))
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				link := regexp.MustCompile(`href="(goroutine[^"]*)"`).FindSubmatch(body)
				require.Len(t, link, 2)
				reference, err := url.Parse(string(link[1]))
				require.NoError(t, err)
				require.Equal(t, prefix+"/debug/pprof/goroutine", response.Request.URL.ResolveReference(reference).Path)
			})

			t.Run("named profile", func(t *testing.T) {
				response, err := client.Get(baseURL + "/debug/pprof/goroutine?debug=1")
				require.NoError(t, err)
				defer response.Body.Close()
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, response.Header.Get("Location"))
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "goroutine profile:")
			})

			t.Run("discovery keeps backend paths", func(t *testing.T) {
				response, err := client.Get(baseURL + "/discovery")
				require.NoError(t, err)
				defer response.Body.Close()
				require.Equal(t, http.StatusOK, response.StatusCode)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "GET /metrics\n")
				require.Contains(t, string(body), "GET /debug/pprof/*\n")
				require.NotContains(t, string(body), "/controller/")
			})
		})
	}
}

func startDiagnosticAPIServer(t *testing.T, registerDefaultRoutes bool) string {
	t.Helper()
	t.Setenv("FLANT_KUBE_CLIENT_IN_MEMORY_DISCOVERY_CACHE", "true")
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	// Client initialization needs a config, but these HTTP routes make no Kubernetes requests.
	require.NoError(t, os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
clusters:
- name: diagnostic-test
  cluster:
    server: http://127.0.0.1:1
contexts:
- name: diagnostic-test
  context:
    cluster: diagnostic-test
current-context: diagnostic-test
`), 0600))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine := shell_operator.NewBareShellOperator(ctx,
		shell_operator.WithLogger(log.NewNop()),
		shell_operator.WithMetricStorage(metricsstorage.NewMetricStorage(metricsstorage.WithNewRegistry())),
	)
	kubeConfig := shell_operator.KubeClientConfig{Config: kubeconfig}
	require.NoError(t, engine.AssembleCommonOperator("127.0.0.1", port, nil, kubeConfig, kubeConfig))
	op := &AddonOperator{ctx: ctx, engine: engine, ConvergeState: converge.NewConvergeState()}
	if registerDefaultRoutes {
		op.registerDefaultRoutes()
	}
	op.StartAPIServer()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, engine.APIServer.Shutdown(ctx))
	})

	serverURL := "http://" + address
	require.Eventually(t, func() bool {
		response, err := http.Get(serverURL + "/discovery")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)
	return serverURL
}

func diagnosticProxyURL(t *testing.T, serverURL, prefix string) string {
	t.Helper()
	if prefix == "" {
		return serverURL
	}
	target, err := url.Parse(serverURL)
	require.NoError(t, err)
	proxy := httptest.NewServer(http.StripPrefix(prefix, httputil.NewSingleHostReverseProxy(target)))
	t.Cleanup(proxy.Close)
	return proxy.URL + prefix
}
