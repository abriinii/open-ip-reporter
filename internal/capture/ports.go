package capture

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DefaultPorts is the candidate list the capture binds when no -ports flag is
// given.
//
// Why a list and not "everything": receiving a UDP broadcast without a packet
// sniffing driver (libpcap / Npcap) requires actually binding the destination
// port. Npcap is a runtime install and an admin prompt on Windows, which the
// project rules out, so instead we bind a wide set of plausible ports at once.
//
// If a miner turns out to broadcast on a port that is not in here, the `sniff`
// subcommand prints a tcpdump line that sees every port with no install
// required. One run of that tells us the port permanently, and it gets added
// to this list.
var DefaultPorts = []int{
	// --- Known ---
	14235, // Antminer / Bitmain IP Reporter. Confirmed.

	// Immediate neighborhood of the Bitmain port. Vendors frequently put a
	// second service, or a newer firmware revision, one or two ports over.
	14234, 14236, 14237, 14238, 14239, 14240,
	14200, 14201, 14202, 14210, 14220, 14230, 14245, 14250, 14255, 14260,

	// --- Mining-specific / vendor tooling ---
	4028, 4029, 4030, // cgminer & bmminer API family (TCP normally, UDP seen)
	4433, 4444,
	3333, 3334, // stratum-adjacent
	8888, 8889, 8890, // very common MicroBT / WhatsMinerTool candidates
	9999, 9998, 9997,
	9527,  // widespread Chinese-vendor device discovery port
	34952, // seen in embedded miner control planes
	60000, 60001,
	11000, 11010,

	// --- Chinese IoT / serial-to-WiFi module discovery ---
	// Whatsminer control boards are ARM SoCs; these ports show up constantly
	// in that ecosystem's "find my device on the LAN" broadcasts.
	48899, // HF-LPB100 / USR-WIFI232 discovery. Strong candidate.
	48891, 48892,
	30303, // Digi / Lantronix style device discovery
	18888,
	58899,
	8000, 8001, 8080, 8081, 8090,

	// --- Generic / catch-all app ports worth a socket ---
	1024, 1025, 1026, 1027, 1028,
	2000, 2001, 2002,
	5000, 5001, 5002,
	6000, 6666, 6667,
	7777, 7778,
	9000, 9001,
	10000, 10001,
	12345, 12346,
	20000, 20001,
	50000, 50001,
	5678, 5679,
	1314, 1315, // "IP report" ports in some rebadged firmware

	// --- Standard protocols ---
	// Not miner IP-report traffic, but genuinely useful: a miner that just
	// powered up will DHCP, and mDNS/NetBIOS/SSDP often carry the hostname
	// and MAC. If the vendor broadcast is a dead end, these are a second way
	// to see the machine announce itself.
	67, 68, // DHCP server / client
	5353,     // mDNS
	1900,     // SSDP
	137, 138, // NetBIOS name / datagram
	161,  // SNMP
	123,  // NTP
	5355, // LLMNR
}

// ParsePorts accepts a comma-separated list of ports and inclusive ranges,
// e.g. "14235,8888,14200-14300". Used by the -ports and -add flags.
func ParsePorts(s string) ([]int, error) {
	var out []int
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(field, "-")
		if !isRange {
			p, err := strconv.Atoi(field)
			if err != nil || p < 1 || p > 65535 {
				return nil, fmt.Errorf("bad port %q", field)
			}
			out = append(out, p)
			continue
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || a < 1 || b > 65535 || a > b {
			return nil, fmt.Errorf("bad port range %q", field)
		}
		if b-a > 4000 {
			return nil, fmt.Errorf("range %q is too wide (max 4000 ports at once)", field)
		}
		for p := a; p <= b; p++ {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ports parsed from %q", s)
	}
	return Dedup(out), nil
}

// Dedup sorts and removes duplicates so we never try to bind the same port twice.
func Dedup(ports []int) []int {
	seen := make(map[int]bool, len(ports))
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}
