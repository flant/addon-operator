package helm_resources_manager

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cr_cache "sigs.k8s.io/controller-runtime/pkg/cache"
	cr_client "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/flant/addon-operator/pkg/utils"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/kube-client/manifest"
)

const monitorDelayBase = time.Minute*4 + time.Second*30

type ResourcesMonitor struct {
	ctx    context.Context
	cancel context.CancelFunc
	paused bool

	moduleName       string
	manifests        []manifest.Manifest
	defaultNamespace string

	kubeClient *klient.Client
	cache      cr_cache.Cache
	logLabels  map[string]string

	absentCb func(moduleName string, unexpectedStatus bool, absent []manifest.Manifest, defaultNs string)

	helmStatusGetter func(releaseName string) (revision string, status string, err error)
}

func NewResourcesMonitor(ctx context.Context, kclient *klient.Client, cache cr_cache.Cache) *ResourcesMonitor {
	cctx, cancel := context.WithCancel(ctx)
	return &ResourcesMonitor{
		paused:     false,
		logLabels:  make(map[string]string),
		manifests:  make([]manifest.Manifest, 0),
		ctx:        cctx,
		cancel:     cancel,
		kubeClient: kclient,
		cache:      cache,
	}
}

func (r *ResourcesMonitor) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *ResourcesMonitor) WithLogLabels(logLabels map[string]string) {
	r.logLabels = logLabels
}

func (r *ResourcesMonitor) WithModuleName(name string) {
	r.moduleName = name
	r.logLabels["module"] = name
}

func (r *ResourcesMonitor) WithDefaultNamespace(ns string) {
	r.defaultNamespace = ns
}

func (r *ResourcesMonitor) WithManifests(manifests []manifest.Manifest) {
	r.manifests = manifests
}

func (r *ResourcesMonitor) WithAbsentCb(cb func(string, bool, []manifest.Manifest, string)) {
	r.absentCb = cb
}

func (r *ResourcesMonitor) WithStatusGetter(lastReleaseStatus func(releaseName string) (revision string, status string, err error)) {
	r.helmStatusGetter = lastReleaseStatus
}

// Start creates a timer and check if all deployed manifests are present in the cluster.
func (r *ResourcesMonitor) Start() {
	logEntry := log.WithFields(utils.LabelsToLogFields(r.logLabels)).
		WithField("operator.component", "HelmResourceMonitor")
	go func() {
		rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
		randSecondsDelay := time.Second * time.Duration(rnd.Int31n(60))
		timer := time.NewTicker(monitorDelayBase + randSecondsDelay)

		for {
			select {
			case <-timer.C:
				if r.paused {
					continue
				}
				// Check release status
				status, err := r.GetHelmReleaseStatus(r.moduleName)
				if err != nil {
					logEntry.Errorf("cannot get helm release status: %s", err)
				}

				if status != "deployed" {
					logEntry.Debugf("Helm release %s is in unexpected status: %s", r.moduleName, status)
					if r.absentCb != nil {
						r.absentCb(r.moduleName, true, []manifest.Manifest{}, r.defaultNamespace)
					}
				}

				// Check resources
				absent, err := r.AbsentResources()
				if err != nil {
					logEntry.Errorf("cannot list helm resources: %s", err)
				}

				if len(absent) > 0 {
					logEntry.Debug("Absent resources detected")
					if r.absentCb != nil {
						r.absentCb(r.moduleName, false, absent, r.defaultNamespace)
					}
				} else {
					logEntry.Debug("No absent resources detected")
				}

			case <-r.ctx.Done():
				timer.Stop()
				return
			}
		}
	}()
}

// GetHelmReleaseStatus returns last release status
func (r *ResourcesMonitor) GetHelmReleaseStatus(moduleName string) (string, error) {
	logEntry := log.WithFields(utils.LabelsToLogFields(r.logLabels)).
		WithField("operator.component", "HelmResourceMonitor")
	revision, status, err := r.helmStatusGetter(moduleName)
	if err != nil {
		return "", err
	}
	logEntry.Debugf("Helm release %s, revision %s, status: %s", moduleName, revision, status)
	return status, nil
}

// Pause prevent execution of absent callback
func (r *ResourcesMonitor) Pause() {
	r.paused = true
}

// Resume allows execution of absent callback
func (r *ResourcesMonitor) Resume() {
	r.paused = false
}

func (r *ResourcesMonitor) AbsentResources() ([]manifest.Manifest, error) {
	gvkMap, err := r.buildGVKMap()
	if err != nil {
		return nil, err
	}

	concurrency := make(chan struct{}, 5)

	// Don't use non-buffered channel here.
	// Range on line 156 will read one message from the channel and quit but other goroutines will wait for the channel
	// in 'chan send' status and stuck forever. Also, GC will not grab the channel because it has wait functions.
	resC := make(chan manifestResult, len(gvkMap))

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()

	for nsgvk, manifests := range gvkMap {
		wg.Add(1)
		go r.checkGVKManifests(ctx, &wg, nsgvk, manifests, resC, concurrency)
	}
	go func() {
		wg.Wait()
		close(resC)
	}()

	for res := range resC {
		if res.err != nil {
			return nil, res.err
		}

		// return on the first absent resource, don't wait for all
		if res.hasAbsent {
			return []manifest.Manifest{res.manifest}, nil
		}
	}

	return nil, nil
}

type namespacedGVK struct {
	Namespace string
	GVK       schema.GroupVersionKind
}

// create gvk
func (r *ResourcesMonitor) buildGVKMap() (map[namespacedGVK][]manifest.Manifest, error) {
	gvkMap := make(map[namespacedGVK][]manifest.Manifest)

	for _, m := range r.manifests {
		// Get GVK
		// log.Debugf("%s: discover GVK for apiVersion '%s' kind '%s'...", ei.Monitor.Metadata.DebugName, ei.Monitor.ApiVersion, ei.Monitor.Kind)
		apiRes, err := r.kubeClient.APIResource(m.ApiVersion(), m.Kind())
		if err != nil {
			// log.Errorf("%s: Cannot get GroupVersionKind info for apiVersion '%s' kind '%s' from api-server. Possibly CRD is not created before informers are started. Error was: %v", ei.Monitor.Metadata.DebugName, ei.Monitor.ApiVersion, ei.Monitor.Kind, err)
			return nil, err
		}
		// log.Debugf("%s: GVK for kind '%s' is '%s'", ei.Monitor.Metadata.DebugName, ei.Monitor.Kind, ei.GroupVersionKind.String())

		gvk := schema.GroupVersionKind{
			Group:   apiRes.Group,
			Version: apiRes.Version,
			Kind:    apiRes.Kind,
		}
		ns := m.Namespace(r.defaultNamespace)
		if !apiRes.Namespaced {
			ns = ""
		}
		nsgvk := namespacedGVK{
			Namespace: ns,
			GVK:       gvk,
		}

		gvkMap[nsgvk] = append(gvkMap[nsgvk], m)
	}

	return gvkMap, nil
}

type manifestResult struct {
	hasAbsent bool
	manifest  manifest.Manifest
	err       error
}

func (r *ResourcesMonitor) checkGVKManifests(ctx context.Context, wg *sync.WaitGroup, nsgvk namespacedGVK, manifests []manifest.Manifest, resC chan<- manifestResult, concurrency chan struct{}) {
	defer wg.Done()

	concurrency <- struct{}{}
	defer func() {
		<-concurrency
	}()

	// if context is cancelled - return
	if ctx.Err() != nil {
		return
	}

	existingObjs, err := r.listResources(ctx, nsgvk)
	if err != nil {
		resC <- manifestResult{
			err: err,
		}
		return
	}

	for _, mf := range manifests {
		if _, ok := existingObjs[mf.Name()]; !ok {
			resC <- manifestResult{
				hasAbsent: true,
				manifest:  mf,
			}
			return
		}
	}
}

// list all objects in ns and return names of all existent objects
func (r *ResourcesMonitor) listResources(ctx context.Context, nsgvk namespacedGVK) (map[string]struct{}, error) {
	objList := &v1.PartialObjectMetadataList{}
	objList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   nsgvk.GVK.Group,
		Version: nsgvk.GVK.Version,
		Kind:    nsgvk.GVK.Kind + "List",
	})
	log.Debugf("List objects from cache for %v", nsgvk)
	err := r.cache.List(ctx, objList, cr_client.InNamespace(nsgvk.Namespace))
	if err != nil {
		return nil, fmt.Errorf("couldn't list objects from cache: %v", err)
	}

	existingObjs := make(map[string]struct{}, len(objList.Items))
	for _, eo := range objList.Items {
		existingObjs[eo.GetName()] = struct{}{}
	}

	return existingObjs, nil
}

func (r *ResourcesMonitor) ResourceIds() []string {
	res := make([]string, 0, len(r.manifests))

	for _, m := range r.manifests {
		id := fmt.Sprintf("%s/%s/%s", m.Namespace(r.defaultNamespace), m.Kind(), m.Name())
		res = append(res, id)
	}

	sort.Strings(res)

	return res
}
