// Package context provides context helpers for kube-config-manager.
package context

import "context"

type contextValue string

const (
	kubeConfigManagerDebug contextValue = "kube-config-manager-debug"
)

func WithKubeConfigManagerDebug(ctx context.Context, value bool) context.Context {
	return context.WithValue(ctx, kubeConfigManagerDebug, value)
}

func IsKubeConfigManagerDebug(ctx context.Context) bool {
	val := ctx.Value(kubeConfigManagerDebug)
	if val == nil {
		return false
	}
	debug, ok := val.(bool)
	return ok && debug
}
