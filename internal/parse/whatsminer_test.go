package parse

import "testing"

func TestWhatsminerParsesTheReport(t *testing.T) {
	tests := []struct {
		payload string
		wantIP  string
		wantMAC string
	}{
		{"IP:10.0.0.5MAC:00:0a:35:1b:2c:3d", "10.0.0.5", "00:0a:35:1b:2c:3d"},
		{"IP:22.1.4.19MAC:C4:11:1E:5A:3B:7F", "22.1.4.19", "c4:11:1e:5a:3b:7f"},
		{"IP:192.168.1.100MAC:00-0A-35-1B-2C-3D", "192.168.1.100", "00:0a:35:1b:2c:3d"},
		{"IP:22.1.4.19MAC:00:0a:35:1b:2c:3d\x00", "22.1.4.19", "00:0a:35:1b:2c:3d"},
		{"IP:22.1.4.19MAC:00:0a:35:1b:2c:3d\r\n", "22.1.4.19", "00:0a:35:1b:2c:3d"},
	}
	for _, tc := range tests {
		rep, ok := (Whatsminer{}).Parse(Packet{DstPort: WhatsminerPort, Data: []byte(tc.payload)})
		if !ok {
			t.Errorf("Parse(%q) rejected a valid report", tc.payload)
			continue
		}
		if rep.IP != tc.wantIP || rep.MAC != tc.wantMAC {
			t.Errorf("Parse(%q) = %s / %s, want %s / %s", tc.payload, rep.IP, rep.MAC, tc.wantIP, tc.wantMAC)
		}
		if rep.Vendor != "Whatsminer" {
			t.Errorf("Vendor = %q", rep.Vendor)
		}
	}
}

// A wrong report is worse than no report.
func TestWhatsminerRejectsEverythingElse(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		port    int
	}{
		{"antminer format on the whatsminer port", "21.1.1.43,02:81:f5:ea:e1:db", WhatsminerPort},
		{"right shape on the antminer port", "IP:10.0.0.5MAC:00:0a:35:1b:2c:3d", AntminerPort},
		{"no MAC marker", "IP:10.0.0.5 00:0a:35:1b:2c:3d", WhatsminerPort},
		{"no IP prefix", "10.0.0.5MAC:00:0a:35:1b:2c:3d", WhatsminerPort},
		{"bad ip", "IP:999.0.0.5MAC:00:0a:35:1b:2c:3d", WhatsminerPort},
		{"bad mac", "IP:10.0.0.5MAC:nonsense", WhatsminerPort},
		{"truncated mac", "IP:10.0.0.5MAC:00:0a:35", WhatsminerPort},
		{"empty", "", WhatsminerPort},
		{"unifi beacon", "\x01\x00\x00\x00", WhatsminerPort},
	}
	for _, tc := range cases {
		if rep, ok := (Whatsminer{}).Parse(Packet{DstPort: tc.port, Data: []byte(tc.payload)}); ok {
			t.Errorf("%s: accepted %q as %+v", tc.name, tc.payload, rep)
		}
	}
}

// Both vendors have to come through one listener, which is the entire point of
// replacing two separate tools with one.
func TestBothVendorsDecodeThroughTheSameRegistry(t *testing.T) {
	anti := Packet{DstPort: AntminerPort, Data: []byte("21.1.1.43,02:81:f5:ea:e1:db")}
	whats := Packet{DstPort: WhatsminerPort, Data: []byte("IP:22.1.4.19MAC:00:0a:35:1b:2c:3d")}

	a, ok := Decode(anti)
	if !ok || a.Vendor != "Antminer" || a.MAC != "02:81:f5:ea:e1:db" {
		t.Errorf("antminer decoded as %+v (ok=%v)", a, ok)
	}
	w, ok := Decode(whats)
	if !ok || w.Vendor != "Whatsminer" || w.MAC != "00:0a:35:1b:2c:3d" {
		t.Errorf("whatsminer decoded as %+v (ok=%v)", w, ok)
	}

	names := HandlerNames()
	if len(names) != 2 {
		t.Errorf("handlers = %v, want both vendors registered", names)
	}
}
