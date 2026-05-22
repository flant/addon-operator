package helm_resources_manager

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cr_client "sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeListClient records every List call and replies with the items it was
// pre-populated with. It mimics the relevant slice of *dedupclient.Client's
// surface (the List method); the lister code is the unit under test.
type fakeListClient struct {
	items   []unstructured.Unstructured
	lastNs  string
	lastSel cr_client.MatchingLabels
	lastGVK schema.GroupVersionKind
	err     error
	calls   int
}

func (f *fakeListClient) List(_ context.Context, list cr_client.ObjectList, opts ...cr_client.ListOption) error {
	f.calls++
	if f.err != nil {
		return f.err
	}

	listOpts := &cr_client.ListOptions{}
	for _, o := range opts {
		o.ApplyToList(listOpts)
	}
	f.lastNs = listOpts.Namespace
	if listOpts.LabelSelector != nil {
		// Re-extract MatchingLabels equivalent for assertions. Only
		// equality requirements are produced by cr_client.MatchingLabels,
		// so this round-trip is faithful.
		reqs, _ := listOpts.LabelSelector.Requirements()
		ml := cr_client.MatchingLabels{}
		for _, r := range reqs {
			vals := r.Values().List()
			if len(vals) == 1 {
				ml[r.Key()] = vals[0]
			}
		}
		f.lastSel = ml
	} else {
		f.lastSel = nil
	}

	ul, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return errors.New("fakeListClient: expected *UnstructuredList")
	}
	f.lastGVK = ul.GroupVersionKind()

	// Materialise items the test expects to be returned, applying the
	// list-time label selector exactly the way the real cache would.
	for _, it := range f.items {
		if !labelsMatch(it.GetLabels(), f.lastSel) {
			continue
		}
		ul.Items = append(ul.Items, it)
	}
	return nil
}

func labelsMatch(have map[string]string, want cr_client.MatchingLabels) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// TestDedupClientLister_AppliesListTimeLabelMatch is the regression test
// for the documented contract that the dedup-backed lister re-applies the
// heritage=addon-operator label match at List time. Without this a manual
// same-named object in the cluster would mask a missing module resource.
func TestDedupClientLister_AppliesListTimeLabelMatch(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}

	managed := unstructured.Unstructured{}
	managed.SetGroupVersionKind(gvk)
	managed.SetName("managed-dep")
	managed.SetNamespace("default")
	managed.SetLabels(map[string]string{"heritage": "addon-operator"})

	foreign := unstructured.Unstructured{}
	foreign.SetGroupVersionKind(gvk)
	foreign.SetName("foreign-dep")
	foreign.SetNamespace("default")
	foreign.SetLabels(map[string]string{"heritage": "manual"})

	fc := &fakeListClient{items: []unstructured.Unstructured{managed, foreign}}
	l := NewDedupClientLister(fc, map[string]string{"heritage": "addon-operator"})

	got, err := l.ListNames(context.Background(), gvk, "default")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}

	want := map[string]struct{}{"managed-dep": {}}
	if !reflect.DeepEqual(got, want) {
		// Friendlier diff for the reader.
		var gotNames, wantNames []string
		for k := range got {
			gotNames = append(gotNames, k)
		}
		for k := range want {
			wantNames = append(wantNames, k)
		}
		sort.Strings(gotNames)
		sort.Strings(wantNames)
		t.Errorf("ListNames: got %v, want %v (label match must drop foreign objects at list time)", gotNames, wantNames)
	}
	if fc.lastNs != "default" {
		t.Errorf("namespace passthrough: got %q, want %q", fc.lastNs, "default")
	}
	if fc.lastGVK.Kind != "DeploymentList" {
		t.Errorf("list GVK: got %v, want kind=DeploymentList", fc.lastGVK)
	}
	if v, ok := fc.lastSel["heritage"]; !ok || v != "addon-operator" {
		t.Errorf("label selector at list time: got %v, want heritage=addon-operator", fc.lastSel)
	}
}

// TestDedupClientLister_NoLabelMatchOmitsSelector documents that callers
// who pass an empty/nil label map opt out of list-time filtering. This
// should never happen in production (newHelmResourcesManager always passes
// a non-empty map parsed from app.ExtraLabels) but the lister must not
// silently inject a selector — that would be a hidden gotcha for tests
// and library consumers.
func TestDedupClientLister_NoLabelMatchOmitsSelector(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	a := unstructured.Unstructured{}
	a.SetGroupVersionKind(gvk)
	a.SetName("a")
	a.SetNamespace("ns")

	fc := &fakeListClient{items: []unstructured.Unstructured{a}}
	l := NewDedupClientLister(fc, nil)

	got, err := l.ListNames(context.Background(), gvk, "ns")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if _, ok := got["a"]; !ok {
		t.Error("expected unfiltered list to include all items")
	}
	if fc.lastSel != nil && len(fc.lastSel) > 0 {
		t.Errorf("expected no list-time label selector when labelMatch is empty, got %v", fc.lastSel)
	}
}

// TestDedupClientLister_PropagatesError ensures errors from the underlying
// client surface verbatim (wrapped) so the resources_monitor caller can
// log them and keep the loop alive.
func TestDedupClientLister_PropagatesError(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	fc := &fakeListClient{err: errors.New("boom")}
	l := NewDedupClientLister(fc, nil)

	if _, err := l.ListNames(context.Background(), gvk, "ns"); err == nil {
		t.Error("expected error from underlying client to propagate, got nil")
	}
}

// TestDedupClientLister_LabelMatchIsDefensiveCopy guarantees the lister
// does not capture the caller's map by reference: mutating the original
// after construction must not change the list-time selector.
func TestDedupClientLister_LabelMatchIsDefensiveCopy(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}

	managed := unstructured.Unstructured{}
	managed.SetGroupVersionKind(gvk)
	managed.SetName("svc")
	managed.SetNamespace("ns")
	managed.SetLabels(map[string]string{"heritage": "addon-operator"})

	fc := &fakeListClient{items: []unstructured.Unstructured{managed}}
	src := map[string]string{"heritage": "addon-operator"}
	l := NewDedupClientLister(fc, src)

	// Mutate the source map after construction; the lister must not see it.
	src["heritage"] = "tampered"
	src["extra"] = "x"

	got, err := l.ListNames(context.Background(), gvk, "ns")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if _, ok := got["svc"]; !ok {
		t.Error("expected svc to be returned despite caller mutating its label map post-construction")
	}
}
