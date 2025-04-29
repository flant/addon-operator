package nelm

import "context"

var _ Logger = (*NelmLogger)(nil)

// FIXME(addon-operator): reference implementation here: https://github.com/werf/nelm/blob/72b22bcd3959e4083a41765ae0b7362c22a2d23d/internal/log/logboek_logger.go
// Must be thread-safe.

func NewNelmLogger() *NelmLogger {
	return &NelmLogger{}
}

type NelmLogger struct{}

func (n *NelmLogger) Trace(ctx context.Context, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) TraceStruct(ctx context.Context, obj interface{}, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) TracePush(ctx context.Context, group, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) TracePop(ctx context.Context, group string) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) Debug(ctx context.Context, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) DebugPush(ctx context.Context, group, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) DebugPop(ctx context.Context, group string) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) Info(ctx context.Context, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) InfoPush(ctx context.Context, group, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) InfoPop(ctx context.Context, group string) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) Warn(ctx context.Context, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) WarnPush(ctx context.Context, group, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) WarnPop(ctx context.Context, group string) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) Error(ctx context.Context, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) ErrorPush(ctx context.Context, group, format string, a ...interface{}) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) ErrorPop(ctx context.Context, group string) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) InfoBlock(ctx context.Context, opts BlockOptions, format string, a ...interface{}) error {
	// FIXME(addon-operator): implement this. No need to do all the stuff we do in logboek. Would be
	// fine to just print `----------` in the beginning and the end of the block, no indentation.
	panic("not implemented yet")
}

func (n *NelmLogger) SetLevel(ctx context.Context, lvl Level) {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) Level(ctx context.Context) Level {
	// FIXME(addon-operator): implement this
	panic("not implemented yet")
}

func (n *NelmLogger) AcceptLevel(ctx context.Context, lvl Level) bool {
	// FIXME(addon-operator): implement this
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
