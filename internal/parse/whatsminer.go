package parse

import (
	"net"
	"strings"
)

// WhatsminerPort is the destination port MicroBT machines report to. Different
// port and a different payload shape from Bitmain, which is why a Whatsminer
// press produced nothing until this handler existed: the datagram was arriving
// and being discarded as unrecognised.
const WhatsminerPort = 8888

// Whatsminer decodes MicroBT's IP report.
//
// The payload is ASCII with the two fields run together and no separator
// between them:
//
//	IP:10.0.0.5MAC:00:0a:35:1b:2c:3d
//
// Note the operational difference from Bitmain, which matters more than the
// format: the IPFOUND button has to be held for **more than five seconds**
// before the machine broadcasts anything. A short press does nothing at all.
type Whatsminer struct{}

func (Whatsminer) Name() string { return "Whatsminer" }

func (Whatsminer) Parse(p Packet) (*Report, bool) {
	if p.DstPort != WhatsminerPort {
		return nil, false
	}

	text := strings.TrimRight(string(p.Data), "\x00\r\n \t")
	rest, ok := strings.CutPrefix(text, "IP:")
	if !ok {
		return nil, false
	}
	// The fields are concatenated, so the MAC marker is the only boundary.
	ipStr, macStr, found := strings.Cut(rest, "MAC:")
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
		Vendor: "Whatsminer",
		IP:     ipStr,
		MAC:    hw.String(),
		TS:     p.TS,
	}, true
}
