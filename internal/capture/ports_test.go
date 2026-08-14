package capture

import "testing"

func TestParsePorts(t *testing.T) {
	tests := []struct {
		in   string
		want []int
	}{
		{"14235", []int{14235}},
		{"8888,14235", []int{8888, 14235}},
		{"14235, 8888 ", []int{8888, 14235}},
		{"100-103", []int{100, 101, 102, 103}},
		{"14235,100-102,14235", []int{100, 101, 102, 14235}},
	}
	for _, tc := range tests {
		got, err := ParsePorts(tc.in)
		if err != nil {
			t.Errorf("ParsePorts(%q) returned error: %v", tc.in, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("ParsePorts(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParsePorts(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestParsePortsRejectsBadInput(t *testing.T) {
	bad := []string{
		"",           // nothing to parse
		"0",          // below the valid range
		"65536",      // above the valid range
		"abc",        // not a number
		"200-100",    // reversed range
		"1-60000",    // too many sockets to bind at once
		"14235,junk", // one bad field poisons the list
	}
	for _, in := range bad {
		if got, err := ParsePorts(in); err == nil {
			t.Errorf("ParsePorts(%q) = %v, want error", in, got)
		}
	}
}

// The default list is bound socket-per-port, so a duplicate would mean a
// guaranteed "address already in use" against ourselves.
func TestDefaultPortsAreUniqueAndValid(t *testing.T) {
	seen := map[int]bool{}
	for _, p := range DefaultPorts {
		if p < 1 || p > 65535 {
			t.Errorf("DefaultPorts contains out-of-range port %d", p)
		}
		if seen[p] {
			t.Errorf("DefaultPorts contains duplicate port %d", p)
		}
		seen[p] = true
	}
	if !seen[14235] {
		t.Error("DefaultPorts is missing 14235, the known Antminer IP Reporter port")
	}
}
