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

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/debug"
	"github.com/flant/shell-operator/pkg/hook/types"
	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/kubectl/pkg/cmd/diff"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/exec"
)

const filedManagerName = "kubectl-client-side-apply"

// RegisterDebugGlobalRoutes registers HTTP endpoints for debugging global values and hooks.
// These endpoints provide information about global configuration, values, patches, and hook snapshots.
func (op *AddonOperator) RegisterDebugGlobalRoutes(dbgSrv *debug.Server) {
	// Register endpoint to list all global hooks
	dbgSrv.RegisterHandler(http.MethodGet, "/global/list.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return map[string]interface{}{
			"globalHooks": op.ModuleManager.GetGlobalHooksNames(),
		}, nil
	})

	// Register endpoint to get global values in JSON or YAML format
	dbgSrv.RegisterHandler(http.MethodGet, "/global/values.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetValues(false), nil
	})

	// Register endpoint to get global config values in JSON or YAML format
	dbgSrv.RegisterHandler(http.MethodGet, "/global/config.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetConfigValues(false), nil
	})

	// Register endpoint to get global values patches in JSON or YAML format
	dbgSrv.RegisterHandler(http.MethodGet, "/global/patches.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GetGlobal().GetValuesPatches(), nil
	})

	// Register endpoint to get snapshots of Kubernetes event hooks in JSON or YAML format
	dbgSrv.RegisterHandler(http.MethodGet, "/global/snapshots.{format:(json|yaml)}",
		func(_ *http.Request) (interface{}, error) {
			// Get all global hooks that respond to Kubernetes events
			kubeHookNames := op.ModuleManager.GetGlobalHooksInOrder(types.OnKubernetesEvent)
			snapshots := make(map[string]interface{})

			// Collect snapshot data for each hook
			for _, hName := range kubeHookNames {
				h := op.ModuleManager.GetGlobalHook(hName)
				snapshots[hName] = h.GetHookController().SnapshotsDump()
			}

			return snapshots, nil
		})
}

func (op *AddonOperator) RegisterDebugGraphRoutes(dbgSrv *debug.Server) {
	dbgSrv.Router.Get("/graph", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

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

		image, err := op.ModuleManager.GetGraphImage(ctx)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(fmt.Sprintf("couldn't get graph's image: %s", err)))
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
	// List enabled modules
	dbgSrv.RegisterHandler(http.MethodGet, "/module/list.{format:(json|yaml|text)}", op.handleModuleList)

	// Get module config or values
	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/{type:(config|values)}.{format:(json|yaml)}", op.handleModuleValues)

	// Render module templates
	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/render", op.handleModuleRender)

	// Get module patches
	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/patches.json", op.handleModulePatches)

	// Get resource monitor information
	dbgSrv.RegisterHandler(http.MethodGet, "/module/resource-monitor.{format:(json|yaml)}", op.handleResourceMonitor)

	// Get module snapshots
	dbgSrv.RegisterHandler(http.MethodGet, "/module/{name}/snapshots.{format:(json|yaml)}", op.handleModuleSnapshots)
}

// Handler functions
func (op *AddonOperator) handleModuleList(_ *http.Request) (interface{}, error) {
	mods := op.ModuleManager.GetEnabledModuleNames()
	sort.Strings(mods)
	return map[string][]string{"enabledModules": mods}, nil
}

func (op *AddonOperator) handleModuleValues(r *http.Request) (interface{}, error) {
	modName := chi.URLParam(r, "name")
	valType := chi.URLParam(r, "type")

	withGlobal, _ := strconv.ParseBool(r.URL.Query().Get("global"))

	m := op.ModuleManager.GetModule(modName)
	if m == nil {
		return nil, fmt.Errorf("module not found")
	}

	if withGlobal {
		global := op.ModuleManager.GetGlobal()
		moduleKey := utils.ModuleNameToValuesKey(m.GetName())

		if valType == "config" {
			return utils.Values{
				"global":  global.GetConfigValues(false),
				moduleKey: m.GetConfigValues(false),
			}, nil
		}

		return utils.Values{
			"global":  global.GetValues(false),
			moduleKey: m.GetValues(false),
		}, nil
	}

	if valType == "config" {
		return m.GetConfigValues(false), nil
	}

	return m.GetValues(false), nil
}

func (op *AddonOperator) handleModuleRender(r *http.Request) (interface{}, error) {
	modName := chi.URLParam(r, "name")
	debugMode, _ := strconv.ParseBool(r.URL.Query().Get("debug"))
	diffMode, _ := strconv.ParseBool(r.URL.Query().Get("diff"))

	m := op.ModuleManager.GetModule(modName)
	if m == nil {
		return nil, fmt.Errorf("module not found")
	}

	deps := &modules.HelmModuleDependencies{
		HelmClientFactory: op.Helm,
	}

	logger := op.Logger.Named("helm-module")
	hm, err := modules.NewHelmModule(m, op.DefaultNamespace, op.ModuleManager.TempDir, deps, nil, modules.WithLogger(logger))
	// if module is not helm, success empty result
	if err != nil && errors.Is(err, modules.ErrModuleIsNotHelm) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create helm module: %w", err)
	}

	releaseManifests, err := hm.Render(app.Namespace, debugMode)
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
				FieldManager:    filedManagerName,
			}

			err = differ.Diff(obj, printer, false)
			if !(err != nil && apierrors.IsConflict(err)) {
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
}

func (op *AddonOperator) handleModulePatches(r *http.Request) (interface{}, error) {
	modName := chi.URLParam(r, "name")

	m := op.ModuleManager.GetModule(modName)
	if m == nil {
		return nil, fmt.Errorf("unknown module %s", modName)
	}

	return m.GetValuesPatches(), nil
}

func (op *AddonOperator) handleResourceMonitor(_ *http.Request) (interface{}, error) {
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
}

func (op *AddonOperator) handleModuleSnapshots(r *http.Request) (interface{}, error) {
	modName := chi.URLParam(r, "name")

	m := op.ModuleManager.GetModule(modName)
	if m == nil {
		return nil, fmt.Errorf("module not found")
	}

	snapshots := make(map[string]interface{})
	for _, h := range m.GetHooks() {
		snapshots[h.GetName()] = h.GetHookController().SnapshotsDump()
	}

	return snapshots, nil
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
			return nil, fmt.Errorf("chi walk: %w", err)
		}

		return buf, nil
	})
}
