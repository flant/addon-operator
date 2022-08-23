package addon_operator

import (
	"fmt"
	"net/http"
	"os"

	"github.com/flant/shell-operator/pkg/debug"
	"github.com/flant/shell-operator/pkg/hook/types"
	"github.com/go-chi/chi/v5"

	"github.com/flant/addon-operator/pkg/app"
)

func RegisterDebugGlobalRoutes(dbgSrv *debug.Server, op *AddonOperator) {
	dbgSrv.Route("/global/list.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return map[string]interface{}{
			"globalHooks": op.ModuleManager.GetGlobalHooksNames(),
		}, nil
	})

	dbgSrv.Route("/global/values.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalValues()
	})

	dbgSrv.Route("/global/config.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalConfigValues(), nil
	})

	dbgSrv.Route("/global/patches.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalValuesPatches(), nil
	})

	dbgSrv.Route("/global/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		kubeHookNames := op.ModuleManager.GetGlobalHooksInOrder(types.OnKubernetesEvent)
		snapshots := make(map[string]interface{})
		for _, hName := range kubeHookNames {
			h := op.ModuleManager.GetGlobalHook(hName)
			snapshots[hName] = h.HookController.SnapshotsDump()
		}

		return snapshots, nil
	})
}

func RegisterDebugModuleRoutes(dbgSrv *debug.Server, op *AddonOperator) {
	dbgSrv.Route("/module/list.{format:(json|yaml|text)}", func(_ *http.Request) (interface{}, error) {
		return map[string][]string{"enabledModules": op.ModuleManager.GetEnabledModuleNames()}, nil
	})

	dbgSrv.Route("/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")
		valType := chi.URLParam(r, "type")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		switch valType {
		case "config":
			return m.ConfigValues(), nil
		case "values":
			return m.Values()
		}
		return "no values", nil
	})

	dbgSrv.Route("/module/{name}/render", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		valuesPath, err := m.PrepareValuesYamlFile()
		if err != nil {
			return nil, err
		}
		defer os.Remove(valuesPath)

		return op.Helm.NewClient().Render(m.Name, m.Path, []string{valuesPath}, nil, app.Namespace)
	})

	dbgSrv.Route("/module/{name}/patches.json", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Unknown module %s", modName)
		}

		return op.ModuleManager.ModuleDynamicValuesPatches(modName), nil
	})

	dbgSrv.Route("/module/resource-monitor.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		dump := map[string]interface{}{}

		for _, moduleName := range op.ModuleManager.GetEnabledModuleNames() {
			if !op.HelmResourcesManager.HasMonitor(moduleName) {
				dump[moduleName] = "No monitor"
				continue
			}

			ids := op.HelmResourcesManager.GetMonitor(moduleName).ResourceIds()
			dump[moduleName] = ids
		}

		return dump, nil
	})

	dbgSrv.Route("/module/{name}/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		mHookNames := op.ModuleManager.GetModuleHookNames(m.Name)
		snapshots := make(map[string]interface{})
		for _, hName := range mHookNames {
			h := op.ModuleManager.GetModuleHook(hName)
			snapshots[hName] = h.HookController.SnapshotsDump()
		}

		return snapshots, nil
	})
}
