package parse

import (
	"net"
	"strings"
)

// AntminerPort is the destination port Bitmain's IP Reporter listens on.
const AntminerPort = 14235

// AntminerSourcePort is the fixed source port observed on real reports. Miners
// send from 14236 to 14235 rather than from an ephemeral port. Treated as a
// corroborating signal only, never a requirement, since firmware revisions we
// have not seen could differ.
const AntminerSourcePort = 14236

// Antminer decodes Bitmain's IP report.
//
// The payload is plain ASCII with no framing of any kind, just the IP and the
// MAC separated by a comma:
//
//	21.1.1.43,02:81:f5:ea:e1:db
//
// Confirmed against live captures on 2026-08-13. Note there is no serial
// number in the report — that is why physical position has to be recorded
// during the walk, and cannot be recovered afterwards.
type Antminer struct{}

func (Antminer) Name() string { return "Antminer" }

func (Antminer) Parse(p Packet) (*Report, bool) {
	if p.DstPort != AntminerPort {
		return nil, false
	}

	// Trim trailing NULs and whitespace: some firmware pads the datagram.
	text := strings.TrimRight(string(p.Data), "\x00\r\n \t")
	ipStr, macStr, found := strings.Cut(text, ",")
	if !found {
		return nil, false
	}
	ipStr = strings.TrimSpace(ipStr)
	macStr = strings.TrimSpace(macStr)

	if net.ParseIP(ipStr) == nil {
		return nil, false
	}
	hw, err := net.ParseMAC(macStr)
	if err != nil {
		return nil, false
	}

	return &Report{
		Vendor: "Antminer",
		IP:     ipStr,
		MAC:    hw.String(), // normalised to lowercase colon form
		TS:     p.TS,
	}, true
}
