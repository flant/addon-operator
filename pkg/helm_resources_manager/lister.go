package helm_resources_manager

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cr_cache "sigs.k8s.io/controller-runtime/pkg/cache"
	cr_client "sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceNameLister returns the set of object names that exist for a given
// (gvk, namespace) tuple, filtered by whatever scope the implementation
// represents. It intentionally collapses cache-implementation detail so the
// resources_monitor consumer never sees the difference between the
// dedicated controller-runtime cache and the runtime DedupClient cache.
//
// Implementations MUST:
//   - Apply the addon-operator-only scope (e.g. the heritage=addon-operator
//     label) themselves, so the consumer can compare returned names against
//     module manifests directly.
//   - Tolerate ctx cancellation by returning the wrapped error without
//     mutating state.
//
// The map[string]struct{} return type is chosen because the caller only
// performs membership checks against it; it is cheaper to build and consume
// than a slice and matches the existing call-site shape in
// resources_monitor.checkGVKManifests.
type ResourceNameLister interface {
	ListNames(ctx context.Context, gvk schema.GroupVersionKind, namespace string) (map[string]struct{}, error)
}

// crCacheLister is the legacy lister: a controller-runtime cache.Cache that
// was constructed with a DefaultLabelSelector (typically
// heritage=addon-operator). It uses *PartialObjectMetadataList, so the
// underlying informer is metadata-only — the cache holds only object
// metadata, not full bodies, which is the smallest possible footprint.
//
// Because the watch is already label-filtered at the cache level, no
// list-time selector is needed.
type crCacheLister struct {
	cache cr_cache.Cache
}

// NewCRCacheLister wraps an existing cr_cache.Cache. Caller owns the cache
// lifecycle (Start/WaitForCacheSync); this lister only reads from it.
func NewCRCacheLister(cache cr_cache.Cache) ResourceNameLister {
	return &crCacheLister{cache: cache}
}

func (l *crCacheLister) ListNames(ctx context.Context, gvk schema.GroupVersionKind, namespace string) (map[string]struct{}, error) {
	objList := &v1.PartialObjectMetadataList{}
	objList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	if err := l.cache.List(ctx, objList, cr_client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("cr-cache list %s in %q: %w", gvk.String(), namespace, err)
	}

	out := make(map[string]struct{}, len(objList.Items))
	for _, eo := range objList.Items {
		out[eo.GetName()] = struct{}{}
	}
	return out, nil
}

// dedupClientLister routes the list-watch through shell-operator's runtime
// DedupClient cache. Compared to crCacheLister:
//
//   - The underlying watch is unfiltered cluster-wide for each GVK (no
//     watch-level label selector in github.com/ldmonster/kubeclient at the
//     time of writing). To preserve absent-resource detection semantics
//     this lister re-applies the heritage label as a list-time
//     MatchingLabels option.
//   - Full *Unstructured bodies live in the cache (the dedup store
//     amortises memory across similar objects, but each body is materialised
//     into UnstructuredList items on List). PartialObjectMetadata is not
//     supported by the upstream cache, so we cannot keep the metadata-only
//     optimisation here.
//
// Trade off intentionally pushed onto the user via
// app.Config.DedupClient.HelmResourcesCache.
type dedupClientLister struct {
	// list is anything that satisfies controller-runtime's reader API for
	// List. In production this is a *dedupclient.Client (which embeds
	// client.Client); kept as the smaller interface so this file can stay
	// independent of the dedupclient import and unit tests can plug a
	// fake.
	list listClient

	// labels is the {key: value} match applied at every List call. Empty
	// means no list-time filtering.
	labels map[string]string
}

// listClient captures the single method the dedup-backed lister needs from
// a controller-runtime client.Client. Defining it locally keeps the
// helm_resources_manager package free of any kubeclient/dedupclient import
// and makes the lister trivially fakeable in unit tests.
type listClient interface {
	List(ctx context.Context, list cr_client.ObjectList, opts ...cr_client.ListOption) error
}

// NewDedupClientLister wraps a List-capable client (typically the
// *dedupclient.Client returned by op.DedupClient()) and applies labelMatch
// at list time. labelMatch may be nil/empty to opt out of list-time
// filtering — useful for tests; production callers should always pass the
// heritage=addon-operator label so absent-resource detection considers
// only operator-managed objects.
func NewDedupClientLister(client listClient, labelMatch map[string]string) ResourceNameLister {
	cp := make(map[string]string, len(labelMatch))
	for k, v := range labelMatch {
		cp[k] = v
	}
	return &dedupClientLister{
		list:   client,
		labels: cp,
	}
}

func (l *dedupClientLister) ListNames(ctx context.Context, gvk schema.GroupVersionKind, namespace string) (map[string]struct{}, error) {
	objList := &unstructured.UnstructuredList{}
	objList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	opts := []cr_client.ListOption{cr_client.InNamespace(namespace)}
	if len(l.labels) > 0 {
		opts = append(opts, cr_client.MatchingLabels(l.labels))
	}

	if err := l.list.List(ctx, objList, opts...); err != nil {
		return nil, fmt.Errorf("dedup-client list %s in %q: %w", gvk.String(), namespace, err)
	}

	out := make(map[string]struct{}, len(objList.Items))
	for i := range objList.Items {
		out[objList.Items[i].GetName()] = struct{}{}
	}
	return out, nil
}
