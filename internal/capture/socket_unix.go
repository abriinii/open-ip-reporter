//go:build !windows

package capture

import "syscall"

// setReuse lets our socket share a port with another process already bound to
// it. This is what allows the capture to run at the same time as Bitmain's own
// IP Reporter on 14235 — press a miner's button and both tools react, which is
// how you confirm the capture is genuinely seeing the real traffic.
func setReuse(fd uintptr) {
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
}

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
