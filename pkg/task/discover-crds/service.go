package discovercrds

import "sync"

type DiscoveredGVKs struct {
	mu             *sync.Mutex
	discoveredGVKs map[string]struct{}
}

func NewDiscoveredGVKs() *DiscoveredGVKs {
	return &DiscoveredGVKs{
		mu:             &sync.Mutex{},
		discoveredGVKs: make(map[string]struct{}),
	}
}

func (d *DiscoveredGVKs) AddGVK(crd string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.discoveredGVKs[crd] = struct{}{}
}

func (d *DiscoveredGVKs) ProcessGVKs(processor func(crdList []string)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.discoveredGVKs) == 0 {
		return
	}

	gvkList := make([]string, 0, len(d.discoveredGVKs))
	for gvk := range d.discoveredGVKs {
		gvkList = append(gvkList, gvk)
	}

	processor(gvkList)
}
