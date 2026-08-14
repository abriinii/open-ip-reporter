package capture

import "testing"

func TestDeriveCan(t *testing.T) {
	// The mapping the site actually uses, confirmed against the switch
	// addresses seen in a live capture.
	tests := map[string]string{
		"11.1.1.254": "A1",
		"12.1.1.254": "A2",
		"13.1.1.254": "A3", // decommissioned can, switch still online
		"14.1.1.254": "A4", // decommissioned can, switch still online
		"15.1.1.254": "A5",
		"16.1.1.254": "A6",
		"17.1.1.254": "A7",
		"18.1.1.254": "A8",
		"21.1.1.254": "B1",
		"22.1.1.254": "B2",
		"23.1.1.254": "B3",
		"24.1.1.254": "B4",
		"31.1.1.254": "O1",
		"32.1.1.254": "O2",
		"33.1.1.254": "O3",
		"34.1.1.254": "O4",
		"15.4.9.113": "A5", // a miner, not just the switch
	}
	for ip, want := range tests {
		if got := DeriveCan(ip); got != want {
			t.Errorf("DeriveCan(%q) = %q, want %q", ip, got, want)
		}
	}
}

// A wrong can label is worse than no label, so anything outside the scheme must
// come back empty rather than guessing.
func TestDeriveCanRejectsAddressesOutsideTheScheme(t *testing.T) {
	for _, ip := range []string{
		"192.168.1.1", // the office router
		"10.0.0.5",    // a flat private range
		"172.16.4.9",
		"41.1.1.254", // no letter mapped to 4
		"20.1.1.254", // x0 is not a can
		"1.1.1.1",
		"255.255.255.255",
		"",
		"not-an-ip",
		"fe80::1",
	} {
		if got := DeriveCan(ip); got != "" {
			t.Errorf("DeriveCan(%q) = %q, want \"\"", ip, got)
		}
	}
}
