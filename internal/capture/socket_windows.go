//go:build windows

package capture

import "syscall"

// setReuse lets our socket share a port with another process already bound to
// it, so the capture can run alongside Bitmain's IP Reporter on 14235.
// Windows has no SO_REUSEPORT; SO_REUSEADDR alone gives the sharing behaviour
// we need for receiving broadcasts.
func setReuse(fd uintptr) {
	syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}

// raiseFDLimit is a no-op on Windows, which has no equivalent per-process
// descriptor limit to raise.
func raiseFDLimit() {}
