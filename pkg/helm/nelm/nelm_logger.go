package nelm

import (
	"context"

	"github.com/werf/nelm/pkg/log"
)

var _ log.Logger = (*NelmLogger)(nil)

func NewNelmLogger() *NelmLogger {
	return &NelmLogger{}
}

// FIXME(addon-operator): implement all methods. Must be thread-safe. Reference implementation here:
// https://github.com/werf/nelm/blob/72b22bcd3959e4083a41765ae0b7362c22a2d23d/internal/log/logboek_logger.go
type NelmLogger struct{}

func (n *NelmLogger) Trace(ctx context.Context, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) TraceStruct(ctx context.Context, obj interface{}, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) TracePush(ctx context.Context, group, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) TracePop(ctx context.Context, group string) {
	panic("not implemented yet")
}

func (n *NelmLogger) Debug(ctx context.Context, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) DebugPush(ctx context.Context, group, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) DebugPop(ctx context.Context, group string) {
	panic("not implemented yet")
}

func (n *NelmLogger) Info(ctx context.Context, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) InfoPush(ctx context.Context, group, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) InfoPop(ctx context.Context, group string) {
	panic("not implemented yet")
}

func (n *NelmLogger) Warn(ctx context.Context, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) WarnPush(ctx context.Context, group, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) WarnPop(ctx context.Context, group string) {
	panic("not implemented yet")
}

func (n *NelmLogger) Error(ctx context.Context, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) ErrorPush(ctx context.Context, group, format string, a ...interface{}) {
	panic("not implemented yet")
}

func (n *NelmLogger) ErrorPop(ctx context.Context, group string) {
	panic("not implemented yet")
}

// FIXME(addon-operator): example implementation
func (n *NelmLogger) InfoBlock(ctx context.Context, opts log.BlockOptions, fn func()) {
	if opts.BlockTitle != "" {
		n.Info(ctx, "----------\n%s\n", opts.BlockTitle)
	}

	n.Info(ctx, "----------\n")
	fn()
	n.Info(ctx, "----------\n")
}

func (n *NelmLogger) InfoBlockErr(ctx context.Context, opts log.BlockOptions, fn func() error) error {
	panic("not implemented yet")
}

func (n *NelmLogger) SetLevel(ctx context.Context, lvl log.Level) {
	panic("not implemented yet")
}

func (n *NelmLogger) Level(ctx context.Context) log.Level {
	panic("not implemented yet")
}

func (n *NelmLogger) AcceptLevel(ctx context.Context, lvl log.Level) bool {
	panic("not implemented yet")
}

func (n *NelmLogger) With(args ...any) *NelmLogger {
	// FIXME(addon-operator): should be like this: https://github.com/deckhouse/deckhouse/blob/f208c10b7aa137410ffb21a9864f8b90aa5cb94b/pkg/log/logger.go#L181
	panic("not implemented yet")
}

func (n *NelmLogger) EnrichWithLabels(labelsMaps ...map[string]string) *NelmLogger {
	// FIXME(addon-operator): should be like this: https://github.com/flant/addon-operator/blob/85252460d4089d60f68321baca1901a50c0fa74b/pkg/utils/merge_labels.go#L22
	panic("not implemented yet")
}
