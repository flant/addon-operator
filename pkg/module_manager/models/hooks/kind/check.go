// Copyright 2025 Flant JSC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kind

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/deckhouse/module-sdk/pkg/settingscheck"
	"github.com/google/uuid"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
)

type SettingsCheck struct {
	path   string
	tmp    string
	logger *log.Logger
}

func NewSettingsCheck(path, tmpPath string, logger *log.Logger) *SettingsCheck {
	return &SettingsCheck{
		path:   path,
		tmp:    tmpPath,
		logger: logger,
	}
}

// Check runs the setting check via the OS interpreter and returns the result of the execution
func (c *SettingsCheck) Check(ctx context.Context, settings utils.Values) (*settingscheck.Result, error) {
	// tmp files has uuid in name and create only in tmp folder (because of RO filesystem)
	tmp, err := c.prepareTmpFile(settings)
	if err != nil {
		return nil, err
	}

	// Remove tmp files after execution
	defer func() {
		if err = os.Remove(tmp); err != nil {
			c.logger.Error("remove tmp file", slog.String("file", tmp), log.Err(err))
		}
	}()

	envs := os.Environ()
	envs = append(envs, fmt.Sprintf("%s=%s", settingscheck.EnvSettingsPath, tmp))

	result := &settingscheck.Result{
		Allow: true,
	}

	cmd := executor.NewExecutor("", c.path, []string{"hook", "check"}, envs).WithLogger(c.logger.Named("executor"))
	if _, err = cmd.RunAndLogLines(ctx, make(map[string]string)); err != nil {
		trimmed := bytes.NewBufferString(strings.TrimPrefix(err.Error(), "stderr:"))

		if err = json.NewDecoder(trimmed).Decode(result); err != nil {
			return nil, fmt.Errorf("parse output: %s", err)
		}
	}

	return result, nil
}

// prepareTmpFile creates temporary files for hook and returns environment variables with paths
func (c *SettingsCheck) prepareTmpFile(settings utils.Values) (string, error) {
	data, err := settings.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(c.tmp, fmt.Sprintf("%s.json", uuid.New().String()))
	if err = utils.DumpData(path, data); err != nil {
		return "", err
	}

	return path, err
}
