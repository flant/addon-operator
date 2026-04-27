package kind

import (
	"github.com/deckhouse/deckhouse/pkg/log"
)

// knownModuleHookKeys lists all top-level keys recognized by the addon-operator
// in a module hook configuration. Unknown keys are silently dropped during
// typed decoding (json/yaml unmarshal ignores unknown fields by default) — so
// without an explicit warning hook authors would not learn that a newly-added
// binding is being ignored at runtime by an older addon-operator.
//
// Forward-compat: when a future module-sdk introduces a new binding, an old
// addon-operator missing this set will emit a warning instead of silently
// ignoring the binding, so operators can spot the version mismatch in logs.
var knownModuleHookKeys = map[string]struct{}{
	// shell-operator core
	"configVersion": {},
	"onStartup":     {},
	"kubernetes":    {},
	"schedule":      {},
	"settings":      {},
	"allowFailure":  {},
	"queue":         {},
	"logLevel":      {},
	// addon-operator module lifecycle
	"beforeHelm":       {},
	"afterHelm":        {},
	"beforeDeleteHelm": {},
	"afterDeleteHelm":  {},
	// addon-operator global lifecycle
	"beforeAll": {},
	"afterAll":  {},
	// batch-hook envelope
	"metadata":           {},
	"name":               {},
	"version":            {},
	"hooks":              {},
	"readiness":          {},
	"has_settings_check": {},
}

// warnUnknownHookKeys emits a warning for each top-level key in raw that is
// not recognized by this version of addon-operator. raw is expected to be the
// JSON/YAML-decoded form of a single hook's config (top-level fields).
func warnUnknownHookKeys(logger *log.Logger, raw map[string]interface{}, hookName string) {
	if logger == nil || len(raw) == 0 {
		return
	}
	for key := range raw {
		if _, ok := knownModuleHookKeys[key]; ok {
			continue
		}
		logger.Warn("unknown key in hook config; addon-operator will ignore it — check addon-operator/module-sdk version compatibility",
			"hook", hookName,
			"key", key,
		)
	}
}
