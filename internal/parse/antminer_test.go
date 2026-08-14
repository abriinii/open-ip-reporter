package parse

import (
	"encoding/hex"
	"testing"
	"time"
)

// The exact bytes of two real reports captured on site, 2026-08-13.
const (
	realReportA = "32312e312e312e34332c30323a38313a66353a65613a65313a6462"
	realReportB = "32312e312e31312e3233322c30323a61643a61663a30323a66663a3435"
)

func packetFromHex(t *testing.T, h string, dstPort int) Packet {
	t.Helper()
	data, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return Packet{
		TS:      time.Date(2026, 8, 13, 23, 30, 21, 0, time.UTC),
		SrcIP:   "21.1.1.43",
		SrcPort: AntminerSourcePort,
		DstPort: dstPort,
		Data:    data,
	}
}

func TestAntminerParsesRealCaptures(t *testing.T) {
	tests := []struct {
		hexData string
		wantIP  string
		wantMAC string
	}{
		{realReportA, "21.1.1.43", "02:81:f5:ea:e1:db"},
		{realReportB, "21.1.11.232", "02:ad:af:02:ff:45"},
	}
	for _, tc := range tests {
		rep, ok := Antminer{}.Parse(packetFromHex(t, tc.hexData, AntminerPort))
		if !ok {
			t.Fatalf("Parse(%s) did not recognise a real Antminer report", tc.hexData)
		}
		if rep.IP != tc.wantIP {
			t.Errorf("IP = %q, want %q", rep.IP, tc.wantIP)
		}
		if rep.MAC != tc.wantMAC {
			t.Errorf("MAC = %q, want %q", rep.MAC, tc.wantMAC)
		}
		if rep.Serial != "" {
			t.Errorf("Serial = %q, want empty: the format carries no serial", rep.Serial)
		}
	}
}

func TestAntminerToleratesPaddingAndCase(t *testing.T) {
	for _, payload := range []string{
		"21.1.1.43,02:81:f5:ea:e1:db",
		"21.1.1.43,02:81:f5:ea:e1:db\x00",
		"21.1.1.43,02:81:f5:ea:e1:db\r\n",
		" 21.1.1.43 , 02:81:F5:EA:E1:DB ",
		"21.1.1.43,02-81-f5-ea-e1-db", // hyphen form, in case firmware differs
	} {
		rep, ok := Antminer{}.Parse(Packet{DstPort: AntminerPort, Data: []byte(payload)})
		if !ok {
			t.Errorf("Parse(%q) failed, want success", payload)
			continue
		}
		if rep.MAC != "02:81:f5:ea:e1:db" {
			t.Errorf("Parse(%q) MAC = %q, want normalised 02:81:f5:ea:e1:db", payload, rep.MAC)
		}
	}
}

// A wrong report is worse than no report, so anything that is not clearly a
// well-formed Antminer announcement must be rejected outright.
func TestAntminerRejectsEverythingElse(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		port    int
	}{
		{"unifi beacon on its own port", "\x01\x00\x00\x00", 10001},
		{"unifi beacon arriving on 14235", "\x01\x00\x00\x00", AntminerPort},
		{"empty", "", AntminerPort},
		{"no comma", "21.1.1.43", AntminerPort},
		{"bad ip", "999.1.1.43,02:81:f5:ea:e1:db", AntminerPort},
		{"bad mac", "21.1.1.43,not-a-mac", AntminerPort},
		{"truncated mac", "21.1.1.43,02:81:f5", AntminerPort},
		{"reversed fields", "02:81:f5:ea:e1:db,21.1.1.43", AntminerPort},
		{"right shape wrong port", "21.1.1.43,02:81:f5:ea:e1:db", 8888},
		{"json from another vendor", `{"mac":"02:81:f5:ea:e1:db"}`, AntminerPort},
	}
	for _, tc := range cases {
		if rep, ok := (Antminer{}).Parse(Packet{DstPort: tc.port, Data: []byte(tc.payload)}); ok {
			t.Errorf("%s: Parse accepted %q as %+v, want rejection", tc.name, tc.payload, rep)
		}
	}
}
