package addon_operator

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/exec"

	"github.com/flant/addon-operator/pkg/addon-operator/diff"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/debug"
	"github.com/flant/shell-operator/pkg/hook/types"
)

const fieldManagerName = "kubectl-client-side-apply"

func (op *AddonOperator) RegisterDebugGlobalRoutes(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/global/list.{format:(json|yaml)}", func(_ *http.Request) (any, error) {
		return map[string]any{
			"globalHooks": op.ModuleManager.GetGlobalHooksNames(),
		}, nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/values.{format:(json|yaml)}", func(_ *http.Request) (any, error) {
		return op.ModuleManager.GetGlobal().GetValues(false), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/config.{format:(json|yaml)}", func(_ *http.Request) (any, error) {
		return op.ModuleManager.GetGlobal().GetConfigValues(false), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/patches.{format:(json|yaml)}", func(_ *http.Request) (any, error) {
		return op.ModuleManager.GetGlobal().GetValuesPatches(), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/global/snapshots.{format:(json|yaml)}",
		func(_ *http.Request) (any, error) {
			kubeHookNames := op.ModuleManager.GetGlobalHooksInOrder(types.OnKubernetesEvent)
			snapshots := make(map[string]any)
			for _, hName := range kubeHookNames {
				h := op.ModuleManager.GetGlobalHook(hName)
				snapshots[hName] = h.GetHookController().SnapshotsDump()
			}

			return snapshots, nil
		})
}

func (op *AddonOperator) RegisterDebugGraphRoutes(dbgSrv *debug.Server) {
	dbgSrv.Router.Get("/graph", func(w http.ResponseWriter, req *http.Request) {
		format := req.URL.Query().Get("format")
		if format == "text" {
			dotDesc, err := op.ModuleManager.GetGraphDOTDescription()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(err.Error()))
				return
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dotDesc)
			return
		}

		image, err := op.ModuleManager.GetGraphImage(req.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "couldn't get graph's image: %s", err)
			return
		}

		buf := new(bytes.Buffer)
		if err = png.Encode(buf, image); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(fmt.Errorf("couldn't encode png graph's image").Error()))
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	})
}

func (op *AddonOperator) RegisterDebugModuleRoutes(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/module/list.{format:(json|yaml|text)}", func(_ *http.Request) (any, error) {
		mods := op.ModuleManager.GetEnabledModuleNames()
		sort.Strings(mods)
		return map[string][]string{"enabledModules": mods}, nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(r *http.Request) (any, error) {
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

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/render", func(r *http.Request) (any, error) {
		modName := chi.URLParam(r, "name")
		debugMode, err := strconv.ParseBool(r.URL.Query().Get("debug"))
		if err != nil {
			// if empty or unparsable - set false
			debugMode = false
		}
		diffMode, err := strconv.ParseBool(r.URL.Query().Get("diff"))
		if err != nil {
			// if empty or unparsable - set false
			diffMode = false
		}

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("module not found")
		}

		deps := &modules.HelmModuleDependencies{
			HelmClientFactory: op.Helm,
		}

		hm, err := modules.NewHelmModule(m, op.DefaultNamespace, op.ModuleManager.TempDir, deps, nil, modules.WithLogger(op.Logger.Named("helm-module")))
		// if module is not helm, success empty result
		if err != nil && errors.Is(err, modules.ErrModuleIsNotHelm) {
			return nil, nil
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create helm module: %w", err)
		}

		releaseManifests, err := hm.Render(app.Namespace, debugMode, m.GetMaintenanceState())
		if err != nil {
			return nil, fmt.Errorf("failed to render manifests: %w", err)
		}

		if !diffMode {
			return releaseManifests, nil
		}

		f, err := os.CreateTemp("", "*")
		if err != nil {
			return nil, fmt.Errorf("failed to write helm chart manifests to a temporary directory: %w", err)
		}

		defer f.Close()

		// file is deleted, yet the file descriptor isn't closed yet and can be accessed
		err = os.Remove(f.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to remove the temp file: %w", err)
		}

		if _, err := f.Write([]byte(releaseManifests)); err != nil {
			return nil, fmt.Errorf("failed to write to the temp file: %w", err)
		}

		res := op.HelmResourcesManager.
			KubeClient().
			NewBuilder().
			Unstructured().
			VisitorConcurrency(3).
			DefaultNamespace().
			FilenameParam(false, &resource.FilenameOptions{
				Filenames: []string{fmt.Sprintf("/proc/self/fd/%d", f.Fd())},
			}).
			Flatten().
			Do()

		if err := res.Err(); err != nil {
			return nil, err
		}

		differ, err := diff.NewDiffer("LIVE", "MERGED")
		if err != nil {
			return nil, err
		}
		defer differ.TearDown()

		const maxRetries = 4
		buffer := new(bytes.Buffer)
		printer := diff.Printer{}
		diffProgram := &diff.DiffProgram{
			Exec: exec.New(),
			IOStreams: genericiooptions.IOStreams{
				In:     os.Stdin,
				Out:    buffer,
				ErrOut: os.Stderr,
			},
		}

		err = res.Visit(func(info *resource.Info, err error) error {
			if err != nil {
				return err
			}

			local := info.Object.DeepCopyObject()
			for i := 1; i <= maxRetries; i++ {
				if err = info.Get(); err != nil {
					if !apierrors.IsNotFound(err) {
						return err
					}
					info.Object = nil
				}

				force := i == maxRetries

				obj := diff.InfoObject{
					LocalObj:        local,
					Info:            info,
					Encoder:         scheme.DefaultJSONEncoder(),
					Force:           force,
					ServerSideApply: true,
					ForceConflicts:  true,
					IOStreams:       diffProgram.IOStreams,
					FieldManager:    fieldManagerName,
				}

				err = differ.Diff(obj, printer, false)
				if err == nil || !apierrors.IsConflict(err) {
					break
				}
			}

			return err
		})
		if err != nil {
			return nil, fmt.Errorf("failed to visit resource: %w", err)
		}

		if err := differ.Run(diffProgram); err != nil {
			if buffer.Len() > 0 {
				return buffer.String(), nil
			}

			return nil, fmt.Errorf("running diff program: %w", err)
		}

		return "No diff found", nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/patches.json", func(r *http.Request) (any, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("unknown module %s", modName)
		}

		return m.GetValuesPatches(), nil
	})

	dbgSrv.RegisterHandler(http.MethodGet, "/module/resource-monitor.{format:(json|yaml)}", func(_ *http.Request) (any, error) {
		dump := map[string]any{}

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

	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/snapshots.{format:(json|yaml)}", func(r *http.Request) (any, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("module not found")
		}

		mHooks := m.GetHooks()
		snapshots := make(map[string]any)
		for _, h := range mHooks {
			snapshots[h.GetName()] = h.GetHookController().SnapshotsDump()
		}

		return snapshots, nil
	})
}

func (op *AddonOperator) RegisterDiscoveryRoute(dbgSrv *debug.Server) {
	dbgSrv.RegisterHandler(http.MethodGet, "/discovery", func(_ *http.Request) (any, error) {
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
			return nil, fmt.Errorf("chi walk: %w", err)
		}

		return buf, nil
	})
}
