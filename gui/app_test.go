package main

import (
	"strings"
	"testing"
	"time"

	"betteripreporter/internal/capture"
	"betteripreporter/internal/export"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.sessionDir = t.TempDir()
	return a
}

func antminerPacket(ip, mac string, ts time.Time) capture.Packet {
	return capture.Packet{
		TS: ts, SrcIP: ip, SrcPort: 14236, DstPort: 14235,
		Data: []byte(ip + "," + mac),
	}
}

// The capture path is the only way a machine gets recorded automatically, so
// the double-fire has to be absorbed here as well as in the offline parser.
func TestCaptureCollapsesDoubleFire(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	base := time.Now()
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", base))
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", base.Add(time.Second)))

	st := a.State()
	if len(st.Entries) != 1 {
		t.Fatalf("recorded %d entries from one button press, want 1", len(st.Entries))
	}
	if st.Entries[0].Label != "C1/1" {
		t.Errorf("first machine at %s, want C1/1", st.Entries[0].Label)
	}
	if st.NextLabel != "C1/2" {
		t.Errorf("next position %s, want C1/2", st.NextLabel)
	}
}

func TestTwoMachinesTakeConsecutivePositions(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	base := time.Now()
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", base))
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", base.Add(time.Second)))
	a.onPacket(antminerPacket("21.1.11.232", "02:ad:af:02:ff:45", base.Add(4*time.Second)))
	a.onPacket(antminerPacket("21.1.11.232", "02:ad:af:02:ff:45", base.Add(5*time.Second)))

	st := a.State()
	if len(st.Entries) != 2 {
		t.Fatalf("recorded %d entries from two presses, want 2", len(st.Entries))
	}
	if st.Entries[0].Label != "C1/1" || st.Entries[1].Label != "C1/2" {
		t.Errorf("positions %s and %s, want C1/1 and C1/2", st.Entries[0].Label, st.Entries[1].Label)
	}
}

// A skip must hold its position open, which is the whole reason it exists.
func TestSkipKeepsFollowingMachinesAligned(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	a.onPacket(antminerPacket("21.1.1.43", "02:00:00:00:00:01", time.Now()))
	a.Skip()
	a.onPacket(antminerPacket("21.1.1.44", "02:00:00:00:00:02", time.Now().Add(5*time.Second)))

	st := a.State()
	if len(st.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(st.Entries))
	}
	if st.Entries[1].Kind != "skipped" {
		t.Errorf("middle entry is %q, want skipped", st.Entries[1].Kind)
	}
	if st.Entries[2].Label != "C1/3" {
		t.Errorf("machine after the skip at %s, want C1/3", st.Entries[2].Label)
	}
}

func TestSessionResumesInsteadOfStartingOver(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", time.Now()))
	a.Skip()

	// Walking away and coming back to the same rack.
	a.StartSession("A5", 1)
	st := a.StartSession("B1", 3)

	if len(st.Entries) != 2 {
		t.Fatalf("resumed with %d entries, want the 2 already recorded", len(st.Entries))
	}
	if st.Entries[0].MAC != "02:81:f5:ea:e1:db" {
		t.Errorf("resumed first MAC %q, want the captured one", st.Entries[0].MAC)
	}
}

// A duplicate never enters the list. Recording it would consume the position
// that belongs to the next real machine and shift the rest of the rack.
func TestDuplicateIsRefusedAndCostsNoPosition(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	const mac = "02:81:f5:ea:e1:db"
	base := time.Now()
	a.onPacket(antminerPacket("21.1.1.43", mac, base))
	// Far outside the double-fire window: a real repeat, not the miner's echo.
	a.onPacket(antminerPacket("21.1.1.43", mac, base.Add(5*time.Minute)))

	st := a.State()
	if len(st.Entries) != 1 {
		t.Fatalf("list holds %d entries, want 1 — the duplicate must not be listed", len(st.Entries))
	}
	if st.NextLabel != "C1/2" {
		t.Errorf("next position is %s, want C1/2 — a refused duplicate must not advance the walk", st.NextLabel)
	}

	// And the next real machine takes the position the duplicate did not.
	a.onPacket(antminerPacket("21.1.1.44", "02:ad:af:02:ff:45", base.Add(6*time.Minute)))
	st = a.State()
	if len(st.Entries) != 2 || st.Entries[1].Label != "C1/2" {
		t.Errorf("next machine landed at %v, want C1/2", st.Entries[1].Label)
	}
}

// Typing in a MAC that is already in the rack is the same mistake.
func TestHandTypedDuplicateIsRefused(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", time.Now()))
	a.Skip()

	st := a.SetMAC(1, "02:81:F5:EA:E1:DB")
	if st.Error == "" {
		t.Fatal("hand-typed duplicate accepted, want refusal")
	}
	if st.Entries[1].Kind != "skipped" {
		t.Error("the refused edit changed the row anyway")
	}
}

// Packets that are not miner reports must never reach the session, or the
// switch beacons alone would fill a rack in under a minute.
func TestNonMinerTrafficIsIgnored(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	for i := 0; i < 50; i++ {
		a.onPacket(capture.Packet{
			TS: time.Now(), SrcIP: "11.1.1.254", SrcPort: 33541,
			DstPort: 10001, Data: []byte{1, 0, 0, 0}, // UniFi beacon
		})
	}
	if n := len(a.State().Entries); n != 0 {
		t.Errorf("recorded %d entries from switch beacons, want 0", n)
	}
}

func TestCapturesWithoutASessionAreDropped(t *testing.T) {
	a := newTestApp(t)
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", time.Now()))
	if a.State().Active {
		t.Error("a capture created a session on its own; the operator must choose the rack")
	}
}

func TestEditingOperationsReachTheEngine(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	base := time.Now()
	a.onPacket(antminerPacket("21.1.1.43", "02:00:00:00:00:01", base))
	a.onPacket(antminerPacket("21.1.1.44", "02:00:00:00:00:02", base.Add(5*time.Second)))
	a.onPacket(antminerPacket("21.1.1.45", "02:00:00:00:00:03", base.Add(10*time.Second)))

	// Delete shifts up.
	st := a.Delete(0)
	if len(st.Entries) != 2 || st.Entries[0].MAC != "02:00:00:00:00:02" {
		t.Fatalf("after delete: %d entries, first %q", len(st.Entries), st.Entries[0].MAC)
	}
	if st.Entries[1].Label != "C1/2" {
		t.Errorf("second entry at %s after delete, want C1/2", st.Entries[1].Label)
	}

	// Insert shifts down.
	st = a.InsertBlankAbove(0)
	if len(st.Entries) != 3 || st.Entries[0].Kind != "skipped" {
		t.Fatalf("after insert: %d entries, first kind %q", len(st.Entries), st.Entries[0].Kind)
	}

	// Pinning renumbers what follows.
	st = a.SetPosition(1, 2, 5)
	if st.Entries[1].Label != "C2/5" {
		t.Errorf("pinned entry at %s, want C2/5", st.Entries[1].Label)
	}
	if st.Entries[2].Label != "C2/6" {
		t.Errorf("entry after pin at %s, want C2/6", st.Entries[2].Label)
	}

	// And undo puts it all back.
	st = a.Undo()
	if st.Entries[1].Label == "C2/5" {
		t.Error("undo did not reverse the pin")
	}
}

func TestBadInputIsReportedNotApplied(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	a.onPacket(antminerPacket("21.1.1.43", "02:00:00:00:00:01", time.Now()))

	if st := a.SetMAC(0, "definitely not a mac"); st.Error == "" {
		t.Error("invalid MAC accepted silently")
	}
	if st := a.SetPosition(0, 99, 99); st.Error == "" {
		t.Error("position outside the rack accepted silently")
	}
	if st := a.Delete(42); st.Error == "" {
		t.Error("deleting a row that does not exist accepted silently")
	}
	if a.State().Entries[0].MAC != "02:00:00:00:00:01" {
		t.Error("a rejected edit still changed the data")
	}
}

// The case notes exist for: a machine that will not report gets skipped, and
// the reason travels with the position into the exported file.
func TestNoteOnASkippedPositionSurvivesExport(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", time.Now()))
	a.Skip()

	st := a.SetNote(1, "wont ip report")
	if st.Error != "" {
		t.Fatalf("SetNote: %s", st.Error)
	}
	if st.Entries[1].Note != "wont ip report" {
		t.Errorf("note is %q", st.Entries[1].Note)
	}
	if st.Entries[1].Kind != "skipped" {
		t.Error("adding a note changed the row's kind")
	}

	var sb strings.Builder
	if err := export.Positional(&sb, a.session); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "wont ip report") {
		t.Errorf("note absent from export:\n%s", sb.String())
	}
}

func TestNoteIsUndoable(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)
	a.Skip()
	a.SetNote(0, "dead PSU")
	if st := a.Undo(); st.Entries[0].Note != "" {
		t.Errorf("after undo note is %q, want empty", st.Entries[0].Note)
	}
}
