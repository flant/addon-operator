package mount_manager

import (
	"os"
	"path/filepath"
	"syscall"
)

type Manager struct {
	mounts map[string]struct{}
	chroot string
}

func NewManager(chroot string) *Manager {
	return &Manager{
		chroot: chroot,
		mounts: make(map[string]struct{}),
	}
}

func (m *Manager) AddDirsToMount(dirs ...string) {
	for _, dir := range dirs {
		m.mounts[dir] = struct{}{}
	}
}

func (m *Manager) PrepareMountsForModule(moduleName string) error {
	chrootedModulePath := filepath.Join(m.chroot, moduleName)
	for dir := range m.mounts {
		chrootedDirPath := filepath.Join(chrootedModulePath, dir)
		if err := os.MkdirAll(chrootedDirPath, 0o755); err != nil {
			return err
		}

		if err := syscall.Mount(dir, chrootedDirPath, "", syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
			return err
		}
	}

	return nil
}
