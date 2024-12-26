package utils

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/app"
)

func GetModuleChrootPath(moduleName string) string {
	if len(app.ShellChrootDir) > 0 {
		return fmt.Sprintf("%s/%s", app.ShellChrootDir, moduleName)
	}

	return ""
}
