package mount_manager

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type MountDescriptor struct {
	Source   string
	Target   string
	Flags    uintptr
	TypeFile bool
}

type Manager struct {
	mounts map[string]MountDescriptor
	chroot string

	l              sync.Mutex
	mountedModules map[string]struct{}
}

func NewManager(chroot string) *Manager {
	return &Manager{
		mountedModules: make(map[string]struct{}),
		chroot:         chroot,
		mounts:         make(map[string]MountDescriptor),
	}
}

func (m *Manager) AddDirsToMount(mounts ...MountDescriptor) {
	for _, mount := range mounts {
		m.mounts[mount.Source] = mount
	}
}

func (m *Manager) PrepareMountsForModule(moduleName, modulePath string) error {
	m.l.Lock()
	defer m.l.Unlock()

	if _, moduleEnvReady := m.mountedModules[moduleName]; moduleEnvReady {
		return nil
	}

	chrootedModuleEnvPath := filepath.Join(m.chroot, moduleName)
	for _, properties := range m.mounts {
		var chrootedMountPath string
		if len(properties.Target) > 0 {
			chrootedMountPath = filepath.Join(chrootedModuleEnvPath, properties.Target)
		} else {
			chrootedMountPath = filepath.Join(chrootedModuleEnvPath, properties.Source)
		}

		if properties.TypeFile {
			if err := os.MkdirAll(filepath.Dir(chrootedMountPath), 0o755); err != nil {
				return err
			}

			bytesRead, err := ioutil.ReadFile(properties.Source)
			if err != nil {
				return err
			}

			if err = ioutil.WriteFile(chrootedMountPath, bytesRead, 0o644); err != nil {
				return err
			}
		} else {
			if err := os.MkdirAll(chrootedMountPath, 0o755); err != nil {
				return err
			}

			if err := syscall.Mount(properties.Source, chrootedMountPath, "", properties.Flags, ""); err != nil {
				return err
			}
		}

	}

	chrootedModuleDir := filepath.Join(chrootedModuleEnvPath, modulePath)
	if err := os.MkdirAll(chrootedModuleDir, 0o755); err != nil {
		return err
	}

	if err := syscall.Mount(modulePath, chrootedModuleDir, "", syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
		return err
	}

	m.mountedModules[moduleName] = struct{}{}

	return nil
}
