package kube_config_manager

import (
	"sync"
)

// Checksums is a thread-safe storage for KubeConfig sections checksums.
type Checksums struct {
	mutex sync.RWMutex
	sums  map[string]map[string]struct{}
}

func NewChecksums() *Checksums {
	return &Checksums{
		sums: make(map[string]map[string]struct{}),
	}
}

func (c *Checksums) ensureName(name string) {
	if c.sums[name] == nil {
		c.sums[name] = make(map[string]struct{})
	}
}

// hasChecksumUnsafe is an internal non-thread-safe version of HasChecksum
func (c *Checksums) hasChecksumUnsafe(name string) bool {
	return len(c.sums[name]) > 0
}

// removeAllUnsafe is an internal non-thread-safe version of RemoveAll
func (c *Checksums) removeAllUnsafe(name string) {
	c.sums[name] = make(map[string]struct{})
}

func (c *Checksums) Add(name string, checksum string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.hasChecksumUnsafe(name) {
		c.removeAllUnsafe(name)
	}
	c.ensureName(name)
	c.sums[name][checksum] = struct{}{}
}

func (c *Checksums) Remove(name string, checksum string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.sums[name] == nil {
		return
	}
	delete(c.sums[name], checksum)
}

// Set saves only one checksum for the name.
func (c *Checksums) Set(name string, checksum string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.removeAllUnsafe(name)
	c.ensureName(name)
	c.sums[name][checksum] = struct{}{}
}

// RemoveAll clears all checksums for the name.
func (c *Checksums) RemoveAll(name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.removeAllUnsafe(name)
}

// Copy copies all checksums for name from src.
func (c *Checksums) Copy(name string, src *Checksums) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Получаем данные из исходного объекта с блокировкой на чтение
	src.mutex.RLock()
	srcData, has := src.sums[name]
	src.mutex.RUnlock()

	if has {
		c.sums[name] = make(map[string]struct{}, len(srcData))
		for k := range srcData {
			c.sums[name][k] = struct{}{}
		}
	}
}

// HasChecksum returns true if there is at least one checksum for name.
func (c *Checksums) HasChecksum(name string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.hasChecksumUnsafe(name)
}

// HasEqualChecksum returns true if there is a checksum for name equal to the input checksum.
func (c *Checksums) HasEqualChecksum(name string, checksum string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	for chk := range c.sums[name] {
		if chk == checksum {
			return true
		}
	}
	return false
}

func (c *Checksums) Names() map[string]struct{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	names := make(map[string]struct{}, len(c.sums))
	for name := range c.sums {
		names[name] = struct{}{}
	}
	return names
}

func (c *Checksums) Dump(moduleName string) map[string]struct{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if checksums, has := c.sums[moduleName]; has {
		result := make(map[string]struct{}, len(checksums))
		for k := range checksums {
			result[k] = struct{}{}
		}
		return result
	}
	return nil
}
