package modules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/config"
	"github.com/flant/shell-operator/pkg/hook/controller"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

// newKubeGoHook returns a go hook with a single OnKubernetesEvent binding.
// Config is precompiled, so no binary is executed on InitializeHookConfig.
func newKubeGoHook(name string) *kind.GoHook {
	gh := kind.NewGoHook(&gohook.HookConfig{
		Kubernetes: []gohook.KubernetesConfig{
			{
				Name:       "pods",
				ApiVersion: "v1",
				Kind:       "Pod",
				FilterFunc: func(_ *unstructured.Unstructured) (gohook.FilterResult, error) {
					return nil, nil
				},
			},
		},
		Logger: log.NewNop(),
	}, func(_ context.Context, _ *gohook.HookInput) error { return nil })

	gh.AddMetadata(&gohook.HookMetadata{Name: name, Path: "/hooks/" + name})

	return gh
}

// newInitializedModuleHook wraps a go hook into ModuleHook and loads its config,
// so GetHookConfig().Bindings() returns OnKubernetesEvent.
func newInitializedModuleHook(t *testing.T, name string) *hooks.ModuleHook {
	t.Helper()

	mh := hooks.NewModuleHook(newKubeGoHook(name))
	require.NoError(t, mh.InitializeHookConfig())

	return mh
}

// TestAddHookIsIdempotentPerBinding proves the byBinding duplication bug:
// re-registering a hook with the same name must replace the old entry,
// not append a second one (Trigger A root cause).
func TestAddHookIsIdempotentPerBinding(t *testing.T) {
	hs := newHooksStorage()

	first := newInitializedModuleHook(t, "hooks/license")
	// A retry creates a fresh object for the same hook (searchModuleHooks
	// calls NewModuleHook on every attempt).
	second := newInitializedModuleHook(t, "hooks/license")

	hs.AddHook(first)
	hs.AddHook(second)

	byName := hs.getHookByName("hooks/license")
	require.Same(t, second, byName, "byName keeps the most recent object")

	byBinding := hs.getHooks(sh_op_types.OnKubernetesEvent)
	require.Len(t, byBinding, 1,
		"byBinding must hold exactly one entry per hook name after re-registration")
	require.Same(t, second, byBinding[0],
		"byBinding entry must be the most recently registered object")
}

// flakyGoHook fails config loading a given number of times.
// Simulates the transient exec failure (ECHILD) that aborted hook
// registration mid-loop in production.
type flakyGoHook struct {
	*kind.GoHook

	mu       sync.Mutex
	failures int
}

func (f *flakyGoHook) GetConfigForModule(moduleKind string) (*config.HookConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failures > 0 {
		f.failures--
		return nil, errors.New("exec file '/hooks/go-hooks-bin': waitid: no child processes")
	}

	return f.GoHook.GetConfigForModule(moduleKind)
}

// TestRegistrationRetryLeavesNoOrphanHooks reproduces the production incident:
// attempt 1 publishes some hooks and fails mid-loop, attempt 2 re-registers
// fresh objects and attaches controllers only to them. Every hook visible via
// GetHooks must have a controller afterwards - a nil one is exactly the value
// that crashed the operator with SIGSEGV.
func TestRegistrationRetryLeavesNoOrphanHooks(t *testing.T) {
	logger := log.NewNop()

	bm, err := NewBasicModule("flant-integration", t.TempDir(), 1, utils.Values{}, nil, nil)
	require.NoError(t, err)
	bm.WithDependencies(stubDeps(logger))

	// Attempt 1: the second hook fails on config load, registerHooks aborts.
	// The first hook is already published to byBinding without a controller.
	okHook1 := hooks.NewModuleHook(newKubeGoHook("hooks/ok"))
	failingHook1 := hooks.NewModuleHook(&flakyGoHook{GoHook: newKubeGoHook("hooks/license"), failures: 1})

	err = bm.registerHooks([]*hooks.ModuleHook{okHook1, failingHook1}, logger)
	require.Error(t, err, "attempt 1 must fail on the failing hook")

	// Attempt 2: ModuleRun retry - fresh hook objects, the failure is gone.
	okHook2 := hooks.NewModuleHook(newKubeGoHook("hooks/ok"))
	failingHook2 := hooks.NewModuleHook(&flakyGoHook{GoHook: newKubeGoHook("hooks/license")})

	err = bm.registerHooks([]*hooks.ModuleHook{okHook2, failingHook2}, logger)
	require.NoError(t, err, "attempt 2 must succeed")

	// RegisterModuleHooks attaches controllers only to hooks returned by the
	// successful attempt, then raises the readiness flag.
	for _, hk := range []*hooks.ModuleHook{okHook2, failingHook2} {
		hk.WithHookController(controller.NewHookController())
	}
	bm.SetHooksControllersReady()

	// Invariant: controllersReady == true implies every hook in byBinding has
	// a controller.
	kubeHooks := bm.GetHooks(sh_op_types.OnKubernetesEvent)
	for _, hk := range kubeHooks {
		assert.NotNilf(t, hk.GetHookController(),
			"hook %q has no controller: stale duplicate left by failed attempt 1", hk.GetName())
	}
	require.Len(t, kubeHooks, 2,
		"byBinding must contain one entry per hook, without stale duplicates")
}

// TestControllersReadyFlagIsRaceFree proves the data race on controllersReady:
// the flag is written by the registration goroutine and read by the kube-events
// dispatcher without synchronization. Run with -race.
func TestControllersReadyFlagIsRaceFree(t *testing.T) {
	bm, err := NewBasicModule("race-flag", t.TempDir(), 1, utils.Values{}, nil, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 1000 {
			bm.SetHooksControllersReady()
		}
	}()

	go func() {
		defer wg.Done()
		for range 1000 {
			_ = bm.HooksControllersReady()
		}
	}()

	wg.Wait()
	require.True(t, bm.HooksControllersReady())
}

// TestConcurrentRegisterHooksIsSafe proves Trigger B entry: two ModuleRun
// workers can both pass the `registered` check before either sets it, so the
// module is registered twice (duplicates + data race on the flag). Run with -race.
func TestConcurrentRegisterHooksIsSafe(t *testing.T) {
	moduleDir := t.TempDir()
	hooksDir := filepath.Join(moduleDir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "startup.sh"), []byte(`#!/bin/sh
if [ "$1" = "--config" ]; then
  echo '{"configVersion":"v1","onStartup":10}'
  exit 0
fi
exit 0
`), 0o755))

	logger := log.NewNop()

	bm, err := NewBasicModule("concurrent", moduleDir, 1, utils.Values{}, nil, nil)
	require.NoError(t, err)
	bm.WithDependencies(stubDeps(logger))

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = bm.RegisterHooks(logger)
		}()
	}
	wg.Wait()

	require.Len(t, bm.GetHooks(sh_op_types.OnStartup), 1,
		"concurrent registration must not produce duplicate hooks")
}
