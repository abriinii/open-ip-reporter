package parse

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCapture builds a .jsonl capture file the way the capture command does.
func writeCapture(t *testing.T, pkts []Packet) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range pkts {
		if err := enc.Encode(map[string]any{
			"ts":        p.TS.Format(time.RFC3339Nano),
			"src_ip":    p.SrcIP,
			"src_port":  p.SrcPort,
			"dst_port":  p.DstPort,
			"hex":       hex.EncodeToString(p.Data),
			"can_guess": "B1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func at(sec float64) time.Time {
	base := time.Date(2026, 8, 13, 23, 30, 0, 0, time.UTC)
	return base.Add(time.Duration(sec * float64(time.Second)))
}

func report(ip, mac string, ts time.Time) Packet {
	return Packet{
		TS: ts, SrcIP: ip, SrcPort: AntminerSourcePort, DstPort: AntminerPort,
		Data: []byte(ip + "," + mac),
	}
}

func beacon(ts time.Time) Packet {
	return Packet{TS: ts, SrcIP: "11.1.1.254", SrcPort: 33541, DstPort: 10001, Data: []byte{1, 0, 0, 0}}
}

// The real sequence from site: two machines, each reporting twice one second
// apart, with switch beacons mixed in.
func TestParseCollapsesTheDoubleFire(t *testing.T) {
	const macA = "02:81:f5:ea:e1:db"
	const macB = "02:ad:af:02:ff:45"

	path := writeCapture(t, []Packet{
		beacon(at(0)),
		beacon(at(10)),
		report("21.1.1.43", macA, at(21.554)),
		report("21.1.1.43", macA, at(22.556)), // the miner's own repeat
		report("21.1.11.232", macB, at(25.232)),
		report("21.1.11.232", macB, at(26.232)), // the miner's own repeat
	})

	reports, stats, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Presses != 2 {
		t.Errorf("Presses = %d, want 2 — one per button press, not one per packet", stats.Presses)
	}
	if stats.Collapsed != 2 {
		t.Errorf("Collapsed = %d, want 2", stats.Collapsed)
	}
	if len(stats.Duplicates) != 0 {
		t.Errorf("Duplicates = %v, want none: a miner firing twice is not a duplicate", stats.Duplicates)
	}
	if stats.Unparsed[10001] != 2 {
		t.Errorf("Unparsed[10001] = %d, want 2 switch beacons", stats.Unparsed[10001])
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if reports[0].MAC != macA || reports[1].MAC != macB {
		t.Errorf("got MACs %q, %q; want %q, %q", reports[0].MAC, reports[1].MAC, macA, macB)
	}
	if reports[0].Copies != 2 {
		t.Errorf("Copies = %d, want 2 packets folded into one press", reports[0].Copies)
	}
	if reports[0].Can != "B1" {
		t.Errorf("Can = %q, want B1 carried through from the capture", reports[0].Can)
	}
}

// The whole point of the window: a repeat outside it is a real duplicate and
// must be flagged, because that is how a site map gets silently corrupted.
func TestParseFlagsGenuineDuplicates(t *testing.T) {
	const mac = "02:81:f5:ea:e1:db"
	path := writeCapture(t, []Packet{
		report("21.1.1.43", mac, at(10)),
		report("21.1.1.43", mac, at(11)),  // double-fire, collapsed
		report("21.1.1.43", mac, at(300)), // walked past it again, five minutes later
		report("21.1.1.43", mac, at(301)), // its double-fire
	})

	reports, stats, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Presses != 2 {
		t.Fatalf("Presses = %d, want 2 distinct presses", stats.Presses)
	}
	if len(stats.Duplicates) != 1 {
		t.Fatalf("Duplicates = %d, want 1", len(stats.Duplicates))
	}
	if stats.Duplicates[0].MAC != mac {
		t.Errorf("flagged %q, want %q", stats.Duplicates[0].MAC, mac)
	}
	if len(reports) != 2 {
		t.Errorf("got %d reports, want both presses kept for the operator to resolve", len(reports))
	}
}

// Two adjacent machines must never be collapsed into one, however close
// together they are pressed.
func TestParseKeepsDistinctMachinesPressedBackToBack(t *testing.T) {
	path := writeCapture(t, []Packet{
		report("21.1.1.43", "02:81:f5:ea:e1:db", at(10.0)),
		report("21.1.1.44", "02:ad:af:02:ff:45", at(10.1)),
		report("21.1.1.45", "02:11:22:33:44:55", at(10.2)),
	})
	reports, stats, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Presses != 3 || len(reports) != 3 {
		t.Fatalf("Presses = %d, reports = %d, want 3 and 3", stats.Presses, len(reports))
	}
	if stats.Collapsed != 0 {
		t.Errorf("Collapsed = %d, want 0: different MACs are different machines", stats.Collapsed)
	}
}

func TestParseFileHandlesEmptyAndGarbage(t *testing.T) {
	empty := writeCapture(t, nil)
	if _, stats, err := ParseFile(empty); err != nil || stats.Presses != 0 {
		t.Errorf("empty capture: err=%v presses=%d, want nil and 0", err, stats.Presses)
	}

	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(bad, []byte("this is not json\n"), 0o644)
	if _, _, err := ParseFile(bad); err == nil {
		t.Error("malformed capture parsed without error, want a clear failure")
	}

	if _, _, err := ParseFile(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Error("missing file parsed without error")
	}
}
