// nolint: goprintffuncname
package nelm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	nelmlog "github.com/werf/nelm/pkg/log"
)

var _ nelmlog.Logger = (*NelmLogger)(nil)

func NewNelmLogger(logger *log.Logger) *NelmLogger {
	return &NelmLogger{
		logger: logger,
	}
}

type NelmLogger struct {
	logger *log.Logger
}

func (n *NelmLogger) Trace(ctx context.Context, format string, a ...interface{}) {
	n.logger.Log(ctx, log.LevelTrace.Level(), fmt.Sprintf(format, a...))
}

func (n *NelmLogger) TraceStruct(ctx context.Context, obj interface{}, format string, a ...interface{}) {
	n.logger.Log(ctx, log.LevelTrace.Level(), fmt.Sprintf(format, a...), slog.Any("obj", obj))
}

func (n *NelmLogger) TracePush(ctx context.Context, _, format string, a ...interface{}) {
	n.logger.Log(ctx, log.LevelTrace.Level(), fmt.Sprintf(format, a...))
}

func (n *NelmLogger) TracePop(_ context.Context, _ string) {
}

func (n *NelmLogger) Debug(ctx context.Context, format string, a ...interface{}) {
	n.logger.DebugContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) DebugPush(ctx context.Context, _, format string, a ...interface{}) {
	n.logger.DebugContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) DebugPop(_ context.Context, _ string) {
}

func (n *NelmLogger) Info(ctx context.Context, format string, a ...interface{}) {
	n.logger.InfoContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) InfoPush(ctx context.Context, _, format string, a ...interface{}) {
	n.logger.InfoContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) InfoPop(_ context.Context, _ string) {
}

func (n *NelmLogger) Warn(ctx context.Context, format string, a ...interface{}) {
	n.logger.WarnContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) WarnPush(ctx context.Context, _, format string, a ...interface{}) {
	n.logger.WarnContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) WarnPop(_ context.Context, _ string) {
}

func (n *NelmLogger) Error(ctx context.Context, format string, a ...interface{}) {
	n.logger.ErrorContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) ErrorPush(ctx context.Context, _, format string, a ...interface{}) {
	n.logger.ErrorContext(ctx, fmt.Sprintf(format, a...))
}

func (n *NelmLogger) ErrorPop(_ context.Context, _ string) {
}

func (n *NelmLogger) InfoBlock(ctx context.Context, opts nelmlog.BlockOptions, fn func()) {
	n.logger.InfoContext(ctx, opts.BlockTitle)

	fn()
}

func (n *NelmLogger) InfoBlockErr(ctx context.Context, opts nelmlog.BlockOptions, fn func() error) error {
	n.logger.InfoContext(ctx, opts.BlockTitle)

	return fmt.Errorf("inner func err: %w", fn())
}

func (n *NelmLogger) SetLevel(_ context.Context, lvl nelmlog.Level) {
	newLvl := log.LevelInfo

	switch lvl {
	case nelmlog.TraceLevel:
		newLvl = log.LevelDebug
	case nelmlog.DebugLevel:
		newLvl = log.LevelDebug
	case nelmlog.InfoLevel:
		newLvl = log.LevelInfo
	case nelmlog.WarningLevel:
		newLvl = log.LevelWarn
	case nelmlog.ErrorLevel:
		newLvl = log.LevelError
	}

	n.logger.SetLevel(newLvl)
}

func (n *NelmLogger) Level(_ context.Context) nelmlog.Level {
	switch n.logger.GetLevel() {
	case log.LevelTrace:
		return nelmlog.DebugLevel
	case log.LevelDebug:
		return nelmlog.DebugLevel
	case log.LevelInfo:
		return nelmlog.InfoLevel
	case log.LevelWarn:
		return nelmlog.WarningLevel
	case log.LevelError:
		return nelmlog.ErrorLevel
	case log.LevelFatal:
		return nelmlog.ErrorLevel
	default:
		return nelmlog.InfoLevel
	}
}

func (n *NelmLogger) AcceptLevel(_ context.Context, _ nelmlog.Level) bool {
	return true
}

func (n *NelmLogger) With(args ...any) *NelmLogger {
	return &NelmLogger{
		logger: n.logger.With(args...),
	}
}

func (n *NelmLogger) EnrichWithLabels(labelsMaps ...map[string]string) *NelmLogger {
	for _, labels := range labelsMaps {
		for k, v := range labels {
			n.logger = n.logger.With(slog.String(k, v))
		}
	}

	return n
}
