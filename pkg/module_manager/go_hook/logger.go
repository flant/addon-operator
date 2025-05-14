package go_hook

import (
	"context"

	"github.com/deckhouse/deckhouse/pkg/log"
	sdkpkg "github.com/deckhouse/module-sdk/pkg"
)

type Logger interface {
	sdkpkg.Logger

	// Deprecated: use Debug instead
	Debugf(format string, args ...any)
	// Deprecated: use Error instead
	Errorf(format string, args ...any)
	// Deprecated: use Fatal instead
	Fatalf(format string, args ...any)
	// Deprecated: use Info instead
	Infof(format string, args ...any)
	// Deprecated: use Log instead
	Logf(ctx context.Context, level log.Level, format string, args ...any)
	// Deprecated: use Warn instead
	Warnf(format string, args ...any)
}
