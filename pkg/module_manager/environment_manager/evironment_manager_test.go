package environment_manager

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
)

const (
	enabledScriptFile1    = "./testdata/enabledScriptFile1"
	enabledScriptFile2    = "./testdata/enabledScriptFile2"
	dstEnabledScriptFile2 = "/path/to/enabledScriptFile2"
	shellHookFile1        = "./testdata/shellHookFile1"
	shellHookFile2        = "./testdata/shellHookFile2"
	dstShellHookFile2     = "/path/to/shellHookFile2"
	chrootDir             = "./testdata/chroot"

	moduleName = "foo-bar"
	modulePath = "./testdata/original/module/foo-bar"
)

func TestEnvironmentManager(t *testing.T) {
	os.Setenv(testsEnv, "true")

	m, uuids, err := prepareTestData()
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 0)
	assert.Equal(t, NoEnvironment, m.preparedEnvironments[moduleName])

	// prepare enabled script environment
	err = m.AssembleEnvironmentForModule(moduleName, modulePath, EnabledScriptEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 1)
	assert.Equal(t, EnabledScriptEnvironment, m.preparedEnvironments[moduleName])

	// check idempotency
	err = m.AssembleEnvironmentForModule(moduleName, modulePath, EnabledScriptEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 1)
	assert.Equal(t, EnabledScriptEnvironment, m.preparedEnvironments[moduleName])

	chrootedModuleEnvPath := filepath.Join(m.chroot, moduleName)

	// check if the module's dir is created
	info, err := os.Stat(chrootedModuleEnvPath)
	assert.NoError(t, err)
	assert.True(t, info.IsDir(), "the module's original directory must be created in the chrooted environment")

	// check enabled script file 1 is copied
	bytesRead, err := os.ReadFile(filepath.Join(chrootedModuleEnvPath, enabledScriptFile1))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile1], string(bytesRead))

	// check enabled script file 2 is copied
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, dstEnabledScriptFile2))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile2], string(bytesRead))

	// check shell hook files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, shellHookFile1))
	assert.True(t, os.IsNotExist(err), "shell hook file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstShellHookFile2))
	assert.True(t, os.IsNotExist(err), "shell hook file 2 mustn't exist")

	// promote to shell hook environment
	err = m.AssembleEnvironmentForModule(moduleName, modulePath, ShellHookEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 1)
	assert.Equal(t, ShellHookEnvironment, m.preparedEnvironments[moduleName])

	// check enabled script file 1 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, enabledScriptFile1))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile1], string(bytesRead))

	// check enabled script file 2 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, dstEnabledScriptFile2))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile2], string(bytesRead))

	// check shell hook file 1 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, shellHookFile1))
	assert.NoError(t, err)
	assert.Equal(t, uuids[shellHookFile1], string(bytesRead))

	// check shell hook file 2 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, dstShellHookFile2))
	assert.NoError(t, err)
	assert.Equal(t, uuids[shellHookFile2], string(bytesRead))

	// demote to enabled script environment
	err = m.DisassembleEnvironmentForModule(moduleName, modulePath, EnabledScriptEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 1)
	assert.Equal(t, EnabledScriptEnvironment, m.preparedEnvironments[moduleName])

	// check enabled script file 1 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, enabledScriptFile1))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile1], string(bytesRead))

	// check enabled script file 2 is in place
	bytesRead, err = os.ReadFile(filepath.Join(chrootedModuleEnvPath, dstEnabledScriptFile2))
	assert.NoError(t, err)
	assert.Equal(t, uuids[enabledScriptFile2], string(bytesRead))

	// check shell hook files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, shellHookFile1))
	assert.True(t, os.IsNotExist(err), "shell hook file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstShellHookFile2))
	assert.True(t, os.IsNotExist(err), "shell hook file 2 mustn't exist")

	// disassemble the environment
	err = m.DisassembleEnvironmentForModule(moduleName, modulePath, NoEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 0)
	assert.Equal(t, NoEnvironment, m.preparedEnvironments[moduleName])

	// check enabled script files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, enabledScriptFile1))
	assert.True(t, os.IsNotExist(err), "enabled script file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstEnabledScriptFile2))
	assert.True(t, os.IsNotExist(err), "enabled script file 2 mustn't exist")

	// check shell hook files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, shellHookFile1))
	assert.True(t, os.IsNotExist(err), "shell hook file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstShellHookFile2))
	assert.True(t, os.IsNotExist(err), "shell hook file 2 mustn't exist")

	// check idempotency
	err = m.DisassembleEnvironmentForModule(moduleName, modulePath, NoEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 0)
	assert.Equal(t, NoEnvironment, m.preparedEnvironments[moduleName])

	// check complete disassembling
	err = m.AssembleEnvironmentForModule(moduleName, modulePath, ShellHookEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 1)
	assert.Equal(t, ShellHookEnvironment, m.preparedEnvironments[moduleName])

	err = m.DisassembleEnvironmentForModule(moduleName, modulePath, NoEnvironment)
	assert.NoError(t, err)
	assert.Len(t, m.preparedEnvironments, 0)
	assert.Equal(t, NoEnvironment, m.preparedEnvironments[moduleName])

	// check enabled script files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, enabledScriptFile1))
	assert.True(t, os.IsNotExist(err), "enabled script file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstEnabledScriptFile2))
	assert.True(t, os.IsNotExist(err), "enabled script file 2 mustn't exist")

	// check shell hook files 1 and 2 don't exist
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, shellHookFile1))
	assert.True(t, os.IsNotExist(err), "shell hook file 1 mustn't exist")
	_, err = os.Stat(filepath.Join(chrootedModuleEnvPath, dstShellHookFile2))
	assert.True(t, os.IsNotExist(err), "shell hook file 2 mustn't exist")

	err = cleanupTestData()
	assert.NoError(t, err)
}

func prepareTestData() (*Manager, map[string]string, error) {
	if err := os.Mkdir("./testdata", 0o750); err != nil {
		return nil, nil, fmt.Errorf("create testdata dir: %w", err)
	}

	uuids := map[string]string{
		enabledScriptFile1: "",
		enabledScriptFile2: "",
		shellHookFile1:     "",
		shellHookFile2:     "",
	}

	for file := range uuids {
		uuid, err := uuid.NewV4()
		if err != nil {
			return nil, nil, fmt.Errorf("generate uuid: %w", err)
		}

		uuids[file] = uuid.String()
		if err = os.WriteFile(file, []byte(uuid.String()), 0o644); err != nil {
			return nil, nil, fmt.Errorf("write uuid to file %s: %w", file, err)
		}
	}

	m := NewManager(chrootDir, log.NewNop())
	m.AddObjectsToEnvironment([]ObjectDescriptor{
		{
			Source:            enabledScriptFile1,
			Type:              File,
			TargetEnvironment: EnabledScriptEnvironment,
		},
		{
			Source:            enabledScriptFile2,
			Type:              File,
			Target:            dstEnabledScriptFile2,
			TargetEnvironment: EnabledScriptEnvironment,
		},
		{
			Source:            shellHookFile1,
			Type:              File,
			TargetEnvironment: ShellHookEnvironment,
		},
		{
			Source:            shellHookFile2,
			Type:              File,
			Target:            dstShellHookFile2,
			TargetEnvironment: ShellHookEnvironment,
		},
	}...)

	return m, uuids, nil
}

func cleanupTestData() error {
	err := os.Remove(enabledScriptFile1)
	if err != nil {
		return fmt.Errorf("delete testdata file %s: %w", enabledScriptFile1, err)
	}

	err = os.Remove(enabledScriptFile2)
	if err != nil {
		return fmt.Errorf("delete testdata file %s: %w", enabledScriptFile2, err)
	}

	err = os.Remove(shellHookFile1)
	if err != nil {
		return fmt.Errorf("delete testdata file %s: %w", shellHookFile1, err)
	}

	err = os.Remove(shellHookFile2)
	if err != nil {
		return fmt.Errorf("delete testdata file %s: %w", shellHookFile2, err)
	}

	err = os.RemoveAll("./testdata")
	if err != nil {
		return fmt.Errorf("delete testdata dir: %w", err)
	}

	return nil
}
