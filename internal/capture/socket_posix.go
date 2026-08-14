//go:build !windows

package capture

import "syscall"

// raiseFDLimit bumps the open-file limit toward the hard cap. We hold one file
// descriptor per bound port, and the default soft limit on macOS is low enough
// that a wide port list would otherwise fail partway through.
func raiseFDLimit() {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return
	}
	if lim.Cur >= lim.Max {
		return
	}
	lim.Cur = lim.Max
	syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
