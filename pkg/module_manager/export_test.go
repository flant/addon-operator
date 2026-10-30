package module_manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
)

func NewModuleManagerForExecutionTest(t *testing.T, ctx context.Context, module *modules.BasicModule, deps ModuleManagerDependencies) *ModuleManager {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("exampleEnabled: true\n"), 0o600))
	extender, err := static.NewExtender(dir)
	require.NoError(t, err)

	mm := NewModuleManager(ctx, &ModuleManagerConfig{
		DirectoryConfig: DirectoryConfig{TempDir: dir},
		Dependencies:    deps,
	}, log.NewNop())
	mm.modules.Add(module)
	require.NoError(t, mm.moduleScheduler.AddModuleVertex(module))
	require.NoError(t, mm.moduleScheduler.AddExtender(extender))
	require.NoError(t, mm.moduleScheduler.ApplyExtenders("Static"))
	require.NoError(t, mm.moduleScheduler.Initialize())
	mm.moduleScheduler.RecalculateGraph(nil)
	require.True(t, mm.IsModuleEnabled(module.GetName()))
	t.Cleanup(mm.Stop)
	return mm
}
