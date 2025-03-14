//go:build !linux

package environment_manager

func MountFn(_ string, _ string, _ string, _ uintptr, _ string, _ bool) error {
	return nil
}
