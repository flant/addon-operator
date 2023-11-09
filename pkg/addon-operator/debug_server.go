package addon_operator

import (
	"fmt"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/debug"
	"github.com/flant/shell-operator/pkg/hook/types"
)

func (op *AddonOperator) RegisterDebugGlobalRoutes(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/global/list.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return map[string]interface{}{
			"globalHooks": op.ModuleManager.GetGlobalHooksNames(),
		}, nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/values.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetValues(), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/config.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetConfigValues(), nil
	})

	//// TODO(yalosev): restore this
	//dbgSrv.RegisterHandler(http.MethodGet, "/global/patches.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
	//	return op.ModuleManager.GlobalValuesPatches(), nil
	//})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		kubeHookNames := op.ModuleManager.GetGlobalHooksInOrder(types.OnKubernetesEvent)
		snapshots := make(map[string]interface{})
		for _, hName := range kubeHookNames {
			h := op.ModuleManager.GetGlobalHook(hName)
			snapshots[hName] = h.GetHookController().SnapshotsDump()
		}

		return snapshots, nil
	})
}

func (op *AddonOperator) RegisterDebugModuleRoutes(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/module/list.{format:(json|yaml|text)}", func(_ *http.Request) (interface{}, error) {
		modules := op.ModuleManager.GetEnabledModuleNames()
		sort.Strings(modules)
		return map[string][]string{"enabledModules": modules}, nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")
		valType := chi.URLParam(r, "type")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		switch valType {
		case "config":
			return m.GetConfigValues(), nil
		case "values":
			return m.GetValues(), nil
		}
		return "no values", nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/render", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")
		dbg, err := strconv.ParseBool(r.URL.Query().Get("debug"))
		if err != nil {
			// if empty or unparsable - set false
			dbg = false
		}

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		deps := &modules.HelmModuleDependencies{
			ClientFactory: op.Helm,
		}
		hm, err := modules.NewHelmModule(op.ModuleManager.GetGlobal().GetValues(), m, op.ModuleManager.TempDir, deps)
		if err != nil {
			return nil, fmt.Errorf("failed to create helm module: %w", err)
		}

		return hm.Render(app.Namespace, dbg)
	})

	// TODO(yalosev): restore me
	//dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/patches.json", func(r *http.Request) (interface{}, error) {
	//	modName := chi.URLParam(r, "name")
	//
	//	m := op.ModuleManager.GetModule(modName)
	//	if m == nil {
	//		return nil, fmt.Errorf("Unknown module %s", modName)
	//	}
	//
	//	return op.ModuleManager.ModuleDynamicValuesPatches(modName), nil
	//})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/resource-monitor.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
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

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		mHooks := m.GetHooks()
		snapshots := make(map[string]interface{})
		for _, h := range mHooks {
			snapshots[h.GetName()] = h.GetHookController().SnapshotsDump()
		}

		return snapshots, nil
	})
}
