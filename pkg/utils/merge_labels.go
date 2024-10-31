package utils

import (
	"log/slog"

	"github.com/deckhouse/deckhouse/go_lib/log"
)

// MergeLabels merges several maps into one. Last map keys overrides keys from first maps.
//
// Can be used to copy a map if just one argument is used.
func MergeLabels(labelsMaps ...map[string]string) map[string]string {
	labels := make(map[string]string)
	for _, labelsMap := range labelsMaps {
		for k, v := range labelsMap {
			labels[k] = v
		}
	}
	return labels
}

func EnrichLoggerWithLabels(logger *log.Logger, labelsMaps ...map[string]string) *log.Logger {
	loggerEntry := logger

	for _, labels := range labelsMaps {
		for k, v := range labels {
			loggerEntry.With(slog.String(k, v))
		}
	}

	return loggerEntry
}
