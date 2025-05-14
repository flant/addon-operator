//go:build !linux

package environment_manager

func MountFn(source string, target string, fstype string, flags uintptr, data string) error {
	return nil
}
