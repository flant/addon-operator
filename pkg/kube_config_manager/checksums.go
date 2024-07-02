package kube_config_manager

// Checksums is a non thread-safe storage for KubeConfig sections checksums.
type Checksums struct {
	sums map[string]map[string]struct{}
}

func NewChecksums() *Checksums {
	return &Checksums{
		sums: make(map[string]map[string]struct{}),
	}
}

func (c *Checksums) Add(name string, checksum string) {
	if !c.HasChecksum(name) {
		c.RemoveAll(name)
	}
	c.sums[name][checksum] = struct{}{}
}

func (c *Checksums) Remove(name string, checksum string) {
	delete(c.sums[name], checksum)
}

// Set saves only one checksum for the name.
func (c *Checksums) Set(name string, checksum string) {
	c.RemoveAll(name)
	c.sums[name][checksum] = struct{}{}
}

// RemoveAll clears all checksums for the name.
func (c *Checksums) RemoveAll(name string) {
	c.sums[name] = make(map[string]struct{})
}

// Copy copies all checksums for name from src.
func (c *Checksums) Copy(name string, src *Checksums) {
	if v, has := src.sums[name]; has {
		c.sums[name] = v
	}
}

// HasChecksum returns true if there is at least one checksum for name.
func (c *Checksums) HasChecksum(name string) bool {
	return len(c.sums[name]) > 0
}

// HasEqualChecksum returns true if there is a checksum for name equal to the input checksum.
func (c *Checksums) HasEqualChecksum(name string, checksum string) bool {
	for chk := range c.sums[name] {
		if chk == checksum {
			return true
		}
	}
	return false
}

func (c *Checksums) Names() map[string]struct{} {
	names := make(map[string]struct{})
	for name := range c.sums {
		names[name] = struct{}{}
	}
	return names
}

func (c *Checksums) Dump(moduleName string) map[string]struct{} {
	if checksums, has := c.sums[moduleName]; has {
		return checksums
	}
	return nil
}
