package app

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
)

// ForcedDurationForDebugLevel - force expiration for debug level.
const (
	ForcedDurationForDebugLevel = 30 * time.Minute
	ProxyJsonLogKey             = "proxyJsonLog"
)

// Registerer is the minimal runtime-config surface SetupLogging needs to
// register a hook that updates the log level on the fly. shell-operator's
// runtime config package implements this interface.
type Registerer interface {
	Register(key string, help string, defaultValue string,
		setter func(key string, newValue string) error,
		expirer func(key string, newValue string) time.Duration)
}

// SetupLogging sets the log level and registers a runtime config hook so the
// level can be changed without restarting the operator. It mirrors the helper
// previously provided by shell-operator's app package so addon-operator no
// longer depends on shell-operator globals for log setup.
func SetupLogging(level string, runtimeConfig Registerer, logger *log.Logger) {
	log.SetDefaultLevel(log.LogLevelFromStr(level))

	runtimeConfig.Register("log.level",
		fmt.Sprintf("Global log level. Default duration for debug level is %s", ForcedDurationForDebugLevel),
		strings.ToLower(level),
		func(_ string, newValue string) error {
			logger.Info("Change log level", slog.String("value", newValue))
			log.SetDefaultLevel(log.LogLevelFromStr(newValue))
			return nil
		}, func(_ string, newValue string) time.Duration {
			if strings.ToLower(newValue) == "debug" {
				return ForcedDurationForDebugLevel
			}
			return 0
		})
}
