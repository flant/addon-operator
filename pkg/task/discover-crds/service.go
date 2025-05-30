package discovercrds

import "sync"

type DiscoveredCRDS struct {
	mu             *sync.Mutex
	discoveredCRDs map[string]struct{}
}

func NewDiscoveredCRDS() *DiscoveredCRDS {
	return &DiscoveredCRDS{
		mu:             &sync.Mutex{},
		discoveredCRDs: make(map[string]struct{}),
	}
}

func (d *DiscoveredCRDS) AddCRD(crd string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.discoveredCRDs[crd] = struct{}{}
}

func (d *DiscoveredCRDS) GetCRDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	crds := make([]string, 0, len(d.discoveredCRDs))
	for crd := range d.discoveredCRDs {
		crds = append(crds, crd)
	}

	return crds
}

func (d *DiscoveredCRDS) WorkWithGVK(fn func(gvks []string)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.discoveredCRDs) == 0 {
		return
	}

	gvks := make([]string, 0, len(d.discoveredCRDs))
	for gvk := range d.discoveredCRDs {
		gvks = append(gvks, gvk)
	}

	fn(gvks)
}
