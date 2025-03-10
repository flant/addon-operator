//go:build linux

package environment_manager

import "syscall"

func MountFn(source string, target string, fstype string, flags uintptr, data string) error {
	if flags == 0 {
		flags = syscall.MS_BIND | syscall.MS_REC
	}

	return syscall.Mount(source, target, fstype, flags, data)
}
