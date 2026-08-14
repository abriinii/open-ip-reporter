//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package capture

import "syscall"

// setReuse lets our socket share a port with another process already bound to
// it. This is what allows the capture to run at the same time as Bitmain's own
// IP Reporter on 14235 — press a miner's button and both tools react, which is
// how you confirm the capture is genuinely seeing the real traffic.
//
// On the BSDs (macOS included) sharing a UDP port for broadcast requires
// SO_REUSEPORT in addition to SO_REUSEADDR. Note that syscall.SO_REUSEPORT is
// only defined on these platforms, which is why Linux gets its own file.
func setReuse(fd uintptr) {
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
}
