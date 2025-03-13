package environment_manager

import "syscall"

func MountFn(source string, target string, fstype string, flags uintptr, data string, recursiveMount bool) error {
	if recursiveMount {
		flags = flags | syscall.MS_BIND | syscall.MS_REC
	}

	return syscall.Mount(source, target, fstype, flags, data)
}
