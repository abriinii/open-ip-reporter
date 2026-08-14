//go:build linux

package capture

import "syscall"

// setReuse sets SO_REUSEADDR only.
//
// Linux would need SO_REUSEPORT to fully share a unicast UDP port, but Go's
// syscall package does not define that constant on Linux and its numeric value
// varies by architecture. Pulling in golang.org/x/sys for one constant is not
// worth it here: port sharing exists so the capture can coexist with Bitmain's
// IP Reporter, and that tool is Windows and macOS only. Linux is a convenience
// target, not a release target.
//
// The practical effect: on Linux, a port already held by another process will
// be reported as unavailable and skipped. Everything else still binds.
func setReuse(fd uintptr) {
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}
