package walk

import (
	"net"
	"strings"
)

// normaliseMAC puts a MAC into one canonical form so that the same machine
// entered by hand and captured off the wire compare equal. Duplicate detection
// is worthless if 02:81:F5:EA:E1:DB and 02-81-f5-ea-e1-db look like two
// different machines.
func normaliseMAC(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	hw, err := net.ParseMAC(s)
	if err != nil {
		return s // keep what the operator typed rather than silently discarding it
	}
	return hw.String()
}

func validMAC(s string) bool {
	_, err := net.ParseMAC(strings.TrimSpace(s))
	return err == nil
}
