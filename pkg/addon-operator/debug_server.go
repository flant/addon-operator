package addon_operator

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
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
		return op.ModuleManager.GetGlobal().GetValues(false), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/config.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetConfigValues(false), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/patches.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetValuesPatches(), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/snapshots.{format:(json|yaml)}",
		func(_ *http.Request) (interface{}, error) {
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
		mods := op.ModuleManager.GetEnabledModuleNames()
		sort.Strings(mods)
		return map[string][]string{"enabledModules": mods}, nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")
		valType := chi.URLParam(r, "type")

		withGlobal := false
		withGlobalStr := r.URL.Query().Get("global")
		v, err := strconv.ParseBool(withGlobalStr)
		if err == nil {
			withGlobal = v
		}

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("module not found")
		}

		if withGlobal {
			global := op.ModuleManager.GetGlobal()

			switch valType {
			case "config":
				return utils.Values{
					"global":                                 global.GetConfigValues(false),
					utils.ModuleNameToValuesKey(m.GetName()): m.GetConfigValues(false),
				}, nil
			case "values":
				return utils.Values{
					"global":                                 global.GetValues(false),
					utils.ModuleNameToValuesKey(m.GetName()): m.GetValues(false),
				}, nil
			}
		} else {
			switch valType {
			case "config":
				return m.GetConfigValues(false), nil
			case "values":
				return m.GetValues(false), nil
			}
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
			HelmClientFactory: op.Helm,
		}
		hm, err := modules.NewHelmModule(m, op.ModuleManager.TempDir, deps, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create helm module: %w", err)
		}

		return hm.Render(app.Namespace, dbg)
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/patches.json", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Unknown module %s", modName)
		}

		return m.GetValuesPatches(), nil
	})

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

func (op *AddonOperator) RegisterDiscoveryRoute(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/discovery", func(_ *http.Request) (interface{}, error) {
		buf := bytes.NewBuffer(nil)
		walkFn := func(
			method string,
			route string,
			_ http.Handler,
			_ ...func(http.Handler) http.Handler,
		) error {
			if strings.HasPrefix(route, "/global/") || strings.HasPrefix(route, "/module/") {
				_, _ = fmt.Fprintf(buf, "%s %s\n", method, route)
				return nil
			}
			return nil
		}

		err := chi.Walk(dbgSrv.Router, walkFn)
		if err != nil {
			return nil, err
		}

		return buf, nil
	})
}
