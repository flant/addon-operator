package environment_manager

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/utils"
)

type (
	Type        string
	Environment int
)

const (
	Mount   Type = "mount"
	File    Type = "file"
	DevNull Type = "devNull"
)

const (
	NoEnvironment Environment = iota
	EnabledScriptEnvironment
	ShellHookEnvironment
)

const testsEnv = "ADDON_OPERATOR_IS_TESTS_ENVIRONMENT"

type ObjectDescriptor struct {
	Source            string
	Target            string
	Flags             uintptr
	Type              Type
	TargetEnvironment Environment
}

type Manager struct {
	objects map[string]ObjectDescriptor
	chroot  string

	l                    sync.Mutex
	preparedEnvironments map[string]Environment

	logger *log.Logger
}

func NewManager(chroot string, logger *log.Logger) *Manager {
	return &Manager{
		preparedEnvironments: make(map[string]Environment),
		chroot:               chroot,
		objects:              make(map[string]ObjectDescriptor),
		logger:               logger,
	}
}

func (m *Manager) AddObjectsToEnvironment(objects ...ObjectDescriptor) {
	m.l.Lock()
	for _, object := range objects {
		m.objects[object.Source] = object
	}
	m.l.Unlock()
}

func makedev(majorNumber int64, minorNumber int64) int {
	return int((majorNumber << 8) | (minorNumber & 0xff) | ((minorNumber & 0xfff00) << 12))
}

func (m *Manager) DisassembleEnvironmentForModule(moduleName, modulePath string, targetEnvironment Environment) error {
	logEntry := utils.EnrichLoggerWithLabels(m.logger, map[string]string{
		"operator.component": "EnvironmentManager.DisassembleEnvironmentForModule",
	})
	m.l.Lock()
	defer m.l.Unlock()

	currentEnvironment := m.preparedEnvironments[moduleName]
	if currentEnvironment == NoEnvironment || currentEnvironment == targetEnvironment {
		return nil
	}

	logEntry.Debug("Disassembling environment",
		slog.String("module", moduleName),
		slog.Any("currentEnvironment", currentEnvironment),
		slog.Any("targetEnvironment", targetEnvironment))

	chrootedModuleEnvPath := filepath.Join(m.chroot, moduleName)
	for _, properties := range m.objects {
		if properties.TargetEnvironment > targetEnvironment && properties.TargetEnvironment <= currentEnvironment {
			var chrootedObjectPath string
			if len(properties.Target) > 0 {
				chrootedObjectPath = filepath.Join(chrootedModuleEnvPath, properties.Target)
			} else {
				chrootedObjectPath = filepath.Join(chrootedModuleEnvPath, properties.Source)
			}

			switch properties.Type {
			case File, DevNull:
				if err := os.Remove(chrootedObjectPath); err != nil {
					return fmt.Errorf("delete file %q: %w", chrootedObjectPath, err)
				}

			case Mount:
				if err := syscall.Unmount(chrootedObjectPath, 0); err != nil {
					return fmt.Errorf("unmount folder %q: %w", chrootedObjectPath, err)
				}
			}
		}
	}

	if targetEnvironment == NoEnvironment {
		if os.Getenv(testsEnv) != "true" {
			chrootedModuleDir := filepath.Join(chrootedModuleEnvPath, modulePath)
			if err := syscall.Unmount(chrootedModuleDir, 0); err != nil {
				return fmt.Errorf("unmount %q module's dir: %w", modulePath, err)
			}
		}

		delete(m.preparedEnvironments, moduleName)
	} else {
		m.preparedEnvironments[moduleName] = targetEnvironment
	}

	return nil
}

func (m *Manager) AssembleEnvironmentForModule(moduleName, modulePath string, targetEnvironment Environment) error {
	logEntry := utils.EnrichLoggerWithLabels(m.logger, map[string]string{
		"operator.component": "EnvironmentManager.AssembleEnvironmentForModule",
	})

	m.l.Lock()
	defer m.l.Unlock()

	currentEnvironment := m.preparedEnvironments[moduleName]
	if currentEnvironment >= targetEnvironment {
		return nil
	}

	logEntry.Debug("Preparing environment",
		slog.String("module", moduleName),
		slog.Any("currentEnvironment", currentEnvironment),
		slog.Any("targetEnvironment", targetEnvironment))

	chrootedModuleEnvPath := filepath.Join(m.chroot, moduleName)

	if currentEnvironment == NoEnvironment {
		logEntry.Debug("Preparing environment - creating the module's directory",
			slog.String("module", moduleName),
			slog.Any("currentEnvironment", currentEnvironment),
			slog.Any("targetEnvironment", targetEnvironment))

		chrootedModuleDir := filepath.Join(chrootedModuleEnvPath, modulePath)
		if err := os.MkdirAll(chrootedModuleDir, 0o755); err != nil {
			return fmt.Errorf("make %q module's dir: %w", modulePath, err)
		}

		if os.Getenv(testsEnv) != "true" {
			if err := MountFn(modulePath, chrootedModuleDir, "", 0, ""); err != nil {
				return fmt.Errorf("mount %q module's dir: %w", modulePath, err)
			}
		}
	}

	for _, properties := range m.objects {
		if properties.TargetEnvironment != currentEnvironment && properties.TargetEnvironment <= targetEnvironment {
			var chrootedObjectPath string
			if len(properties.Target) > 0 {
				chrootedObjectPath = filepath.Join(chrootedModuleEnvPath, properties.Target)
			} else {
				chrootedObjectPath = filepath.Join(chrootedModuleEnvPath, properties.Source)
			}

			switch properties.Type {
			case File:
				if err := os.MkdirAll(filepath.Dir(chrootedObjectPath), 0o755); err != nil {
					return fmt.Errorf("make dir %q: %w", chrootedObjectPath, err)
				}

				bytesRead, err := os.ReadFile(properties.Source)
				if err != nil {
					return fmt.Errorf("read from file %q: %w", properties.Source, err)
				}

				if err = os.WriteFile(chrootedObjectPath, bytesRead, 0o644); err != nil {
					return fmt.Errorf("write to file %q: %w", chrootedObjectPath, err)
				}

			case DevNull:
				if err := os.MkdirAll(filepath.Dir(chrootedObjectPath), 0o755); err != nil {
					return fmt.Errorf("make dir %q: %w", chrootedObjectPath, err)
				}

				if err := syscall.Mknod(chrootedObjectPath, syscall.S_IFCHR|0o666, makedev(1, 3)); err != nil {
					if errors.Is(err, os.ErrExist) {
						continue
					}
					return fmt.Errorf("create null file: %w", err)
				}

				if err := os.Chmod(chrootedObjectPath, 0o666); err != nil {
					return fmt.Errorf("chmod %q file: %w", chrootedObjectPath, err)
				}

			case Mount:
				if err := os.MkdirAll(chrootedObjectPath, 0o755); err != nil {
					return fmt.Errorf("make dir %q: %w", chrootedObjectPath, err)
				}

				if err := MountFn(properties.Source, chrootedObjectPath, "", properties.Flags, ""); err != nil {
					return fmt.Errorf("mount folder %q: %w", chrootedObjectPath, err)
				}
			}
		}
	}

	m.preparedEnvironments[moduleName] = targetEnvironment

	return nil
}
