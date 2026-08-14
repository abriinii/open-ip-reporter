package walk

import (
	"path/filepath"
	"testing"
	"time"
)

var b1 = Geometry{Rows: 10, Columns: 6} // a standard A/B can rack: 60 positions
var o1 = Geometry{Rows: 8, Columns: 6}  // an O can rack: 48 positions

func newTestSession(t *testing.T, g Geometry) *Session {
	t.Helper()
	s, err := NewSession("B1", 3, g)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The walk goes down column 1 top to bottom, then to the top of column 2.
func TestWalkOrderIsColumnMajor(t *testing.T) {
	want := []Position{
		{Can: "B1", Rack: 3, Column: 1, Row: 1},
		{Can: "B1", Rack: 3, Column: 1, Row: 2},
		{Can: "B1", Rack: 3, Column: 1, Row: 10},
		{Can: "B1", Rack: 3, Column: 2, Row: 1},
		{Can: "B1", Rack: 3, Column: 2, Row: 2},
		{Can: "B1", Rack: 3, Column: 6, Row: 10},
	}
	indices := []int{0, 1, 9, 10, 11, 59}
	for k, idx := range indices {
		got, ok := PositionAt("B1", 3, b1, idx)
		if !ok {
			t.Fatalf("PositionAt(%d) not ok", idx)
		}
		if got != want[k] {
			t.Errorf("PositionAt(%d) = %v, want %v", idx, got, want[k])
		}
		if back := got.Index(b1); back != idx {
			t.Errorf("%v.Index() = %d, want %d — Index and PositionAt must be inverses", got, back, idx)
		}
	}
	if _, ok := PositionAt("B1", 3, b1, 60); ok {
		t.Error("PositionAt(60) succeeded on a 60-position rack, want out of range")
	}
}

func TestRackEndsAfterLastPosition(t *testing.T) {
	last := Position{Can: "B1", Rack: 3, Column: 6, Row: 10}
	if _, ok := last.Next(b1); ok {
		t.Error("Next() past the last position succeeded, want end of rack")
	}
	lastO := Position{Can: "O1", Rack: 3, Column: 6, Row: 8}
	if _, ok := lastO.Next(o1); ok {
		t.Error("O-can rack did not end at column 6 row 8")
	}
}

// The heart of it: recording and skipping both consume a position, so a skip
// keeps everything after it aligned.
func TestSkipHoldsThePositionOpen(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:81:f5:ea:e1:db", "21.1.1.43", "Antminer", time.Now())
	s.Skip() // an empty slot
	s.Record("02:ad:af:02:ff:45", "21.1.11.232", "Antminer", time.Now())

	wantRows := []int{1, 2, 3}
	for i, wantRow := range wantRows {
		p, _ := s.PositionOf(i)
		if p.Row != wantRow || p.Column != 1 {
			t.Errorf("entry %d at column %d row %d, want column 1 row %d", i, p.Column, p.Row, wantRow)
		}
	}
	if s.Entries[1].Kind != Skipped {
		t.Error("skip did not record a skipped entry")
	}
}

// Deleting shifts everything below up one position. This is the operation
// needed when a phantom row was recorded.
func TestDeleteShiftsFollowingEntriesUp(t *testing.T) {
	s := newTestSession(t, b1)
	for _, mac := range []string{"02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"} {
		s.Record(mac, "", "Antminer", time.Now())
	}
	// ...:03 sits at row 3.
	if p, _ := s.PositionOf(2); p.Row != 3 {
		t.Fatalf("setup wrong: third entry at row %d", p.Row)
	}

	if err := s.Delete(0); err != nil {
		t.Fatal(err)
	}

	if len(s.Entries) != 2 {
		t.Fatalf("after delete there are %d entries, want 2", len(s.Entries))
	}
	if s.Entries[0].MAC != "02:00:00:00:00:02" {
		t.Errorf("first entry is %q, want the second machine to have moved up", s.Entries[0].MAC)
	}
	if p, _ := s.PositionOf(1); p.Row != 2 {
		t.Errorf("third machine now at row %d, want 2 — everything below a delete shifts up", p.Row)
	}
}

// Inserting a blank shifts everything below down one position. This is the fix
// for a machine missed on the way past.
func TestInsertBlankShiftsFollowingEntriesDown(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:00:00:00:00:01", "", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "", "Antminer", time.Now())

	if err := s.InsertBlankAbove(1); err != nil {
		t.Fatal(err)
	}

	if len(s.Entries) != 3 {
		t.Fatalf("after insert there are %d entries, want 3", len(s.Entries))
	}
	if s.Entries[1].Kind != Skipped {
		t.Error("inserted entry is not a skip")
	}
	if s.Entries[2].MAC != "02:00:00:00:00:02" {
		t.Errorf("entry below the insert is %q, want it pushed down", s.Entries[2].MAC)
	}
	if p, _ := s.PositionOf(2); p.Row != 3 {
		t.Errorf("pushed entry at row %d, want 3", p.Row)
	}
}

// "I'm off by one, fix it in place": pinning one entry must carry to the rest
// of the rack, not just relabel a single row.
func TestSetPositionRenumbersEverythingAfterIt(t *testing.T) {
	s := newTestSession(t, b1)
	for i := 0; i < 5; i++ {
		s.Record("02:00:00:00:00:0"+string(rune('1'+i)), "", "Antminer", time.Now())
	}
	// Entry 2 is really at column 1 row 7, not row 3.
	pin := Position{Can: "B1", Rack: 3, Column: 1, Row: 7}
	if err := s.SetPosition(2, pin); err != nil {
		t.Fatal(err)
	}

	if p, _ := s.PositionOf(2); p != pin {
		t.Errorf("pinned entry at %v, want %v", p, pin)
	}
	if p, _ := s.PositionOf(3); p.Row != 8 {
		t.Errorf("entry after the pin at row %d, want 8 — the correction must carry", p.Row)
	}
	if p, _ := s.PositionOf(4); p.Row != 9 {
		t.Errorf("second entry after the pin at row %d, want 9", p.Row)
	}
	// Entries before the pin are untouched.
	if p, _ := s.PositionOf(0); p.Row != 1 {
		t.Errorf("entry before the pin moved to row %d, want 1", p.Row)
	}
}

func TestSetPositionRejectsPositionsOutsideTheRack(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:00:00:00:00:01", "", "Antminer", time.Now())
	for _, bad := range []Position{
		{Can: "B1", Rack: 3, Column: 7, Row: 1},  // no column 7
		{Can: "B1", Rack: 3, Column: 1, Row: 11}, // no row 11
		{Can: "B1", Rack: 3, Column: 0, Row: 1},
		{Can: "B1", Rack: 3, Column: 1, Row: 0},
	} {
		if err := s.SetPosition(0, bad); err == nil {
			t.Errorf("SetPosition(%v) succeeded, want rejection", bad)
		}
	}
}

func TestSetMACValidatesAndConvertsKind(t *testing.T) {
	s := newTestSession(t, b1)
	s.Skip()

	if err := s.SetMAC(0, "not a mac"); err == nil {
		t.Error("SetMAC accepted rubbish, want rejection")
	}
	if err := s.SetMAC(0, "02:81:F5:EA:E1:DB"); err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].Kind != Reported {
		t.Error("typing a MAC into a skipped row did not make it a reported row")
	}
	if s.Entries[0].MAC != "02:81:f5:ea:e1:db" {
		t.Errorf("MAC stored as %q, want normalised lowercase", s.Entries[0].MAC)
	}
	if err := s.SetMAC(0, ""); err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].Kind != Skipped {
		t.Error("clearing the MAC did not return the row to skipped")
	}
}

// Duplicate detection must be blind to formatting, or it misses the case it
// exists for.
func TestDuplicatesIgnoreMACFormatting(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:81:f5:ea:e1:db", "", "Antminer", time.Now())
	s.Record("02:00:00:00:00:99", "", "Antminer", time.Now())
	s.Record("02-81-F5-EA-E1-DB", "", "Antminer", time.Now()) // same machine, typed differently

	dupes := s.Duplicates()
	if len(dupes) != 1 {
		t.Fatalf("found %d duplicate MACs, want 1: %v", len(dupes), dupes)
	}
	idx := dupes["02:81:f5:ea:e1:db"]
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 2 {
		t.Errorf("duplicate at %v, want positions 0 and 2", idx)
	}
}

func TestSkippedRowsAreNeverDuplicates(t *testing.T) {
	s := newTestSession(t, b1)
	s.Skip()
	s.Skip()
	s.Skip()
	if d := s.Duplicates(); len(d) != 0 {
		t.Errorf("empty slots flagged as duplicates: %v", d)
	}
}

func TestUndoRedoAcrossEveryMutation(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:00:00:00:00:01", "", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "", "Antminer", time.Now())
	s.Skip()
	s.Delete(0)
	s.InsertBlankAbove(0)
	s.SetMAC(0, "02:00:00:00:00:09")
	s.SetPosition(0, Position{Can: "B1", Rack: 3, Column: 2, Row: 2})

	final := len(s.Entries)
	finalFirst := s.Entries[0].MAC

	// Unwind everything.
	steps := 0
	for s.CanUndo() {
		s.Undo()
		steps++
	}
	if steps != 7 {
		t.Errorf("undid %d operations, want 7", steps)
	}
	if len(s.Entries) != 0 {
		t.Errorf("after undoing everything there are %d entries, want 0", len(s.Entries))
	}

	// And put it all back.
	for s.CanRedo() {
		s.Redo()
	}
	if len(s.Entries) != final {
		t.Errorf("after redoing everything there are %d entries, want %d", len(s.Entries), final)
	}
	if s.Entries[0].MAC != finalFirst {
		t.Errorf("redo restored first MAC as %q, want %q", s.Entries[0].MAC, finalFirst)
	}
}

// A snapshot must be a copy. If undo handed back a slice still aliasing live
// state, later edits would corrupt the history silently.
func TestUndoSnapshotsDoNotAliasLiveState(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:00:00:00:00:01", "", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "", "Antminer", time.Now())
	s.SetMAC(1, "02:00:00:00:00:ff")
	s.Undo()
	if s.Entries[1].MAC != "02:00:00:00:00:02" {
		t.Errorf("after undo MAC is %q, want the original 02:00:00:00:00:02", s.Entries[1].MAC)
	}
}

func TestNewMutationClearsRedo(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:00:00:00:00:01", "", "Antminer", time.Now())
	s.Undo()
	if !s.CanRedo() {
		t.Fatal("setup wrong: redo should be available")
	}
	s.Record("02:00:00:00:00:02", "", "Antminer", time.Now())
	if s.CanRedo() {
		t.Error("redo survived a new edit, which would let it reinstate a discarded branch")
	}
}

func TestFullAndNextPosition(t *testing.T) {
	s := newTestSession(t, o1) // 48 positions
	for i := 0; i < 48; i++ {
		s.Skip()
	}
	if !s.Full() {
		t.Error("48 entries in a 48-position rack is not reported as full")
	}
	if _, ok := s.NextPosition(); ok {
		t.Error("NextPosition succeeded on a full rack, want end of rack")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestSession(t, b1)
	s.Record("02:81:f5:ea:e1:db", "21.1.1.43", "Antminer", time.Now())
	s.Skip()
	s.Record("02:ad:af:02:ff:45", "21.1.11.232", "Antminer", time.Now())
	s.SetPosition(2, Position{Can: "B1", Rack: 3, Column: 2, Row: 5})

	path := filepath.Join(t.TempDir(), "sessions", "b1-r3.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Can != "B1" || got.Rack != 3 || got.Geom != b1 {
		t.Errorf("loaded %s rack %d geom %v, want B1 rack 3 geom %v", got.Can, got.Rack, got.Geom, b1)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("loaded %d entries, want 3", len(got.Entries))
	}
	for i := range s.Entries {
		want, _ := s.PositionOf(i)
		gotPos, _ := got.PositionOf(i)
		if want != gotPos {
			t.Errorf("entry %d position %v after reload, want %v", i, gotPos, want)
		}
	}
	if got.Entries[0].MAC != "02:81:f5:ea:e1:db" {
		t.Errorf("MAC survived reload as %q", got.Entries[0].MAC)
	}
}

// Saving repeatedly must never leave a half-written file behind, since that is
// what a lid close mid-walk looks like.
func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	s := newTestSession(t, b1)
	for i := 0; i < 20; i++ {
		s.Skip()
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only the session file", entries)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 20 {
		t.Errorf("reloaded %d entries, want 20", len(got.Entries))
	}
}

func TestNewSessionRejectsNonsense(t *testing.T) {
	if _, err := NewSession("", 1, b1); err == nil {
		t.Error("empty can accepted")
	}
	if _, err := NewSession("B1", 0, b1); err == nil {
		t.Error("rack 0 accepted")
	}
	if _, err := NewSession("B1", 1, Geometry{}); err == nil {
		t.Error("empty geometry accepted")
	}
}

func TestDefaultGeometryMatchesTheSite(t *testing.T) {
	for _, can := range []string{"A1", "A2", "A5", "A6", "A7", "A8", "B1", "B2", "B3", "B4"} {
		g, ok := DefaultGeometry(can)
		if !ok || g.Positions() != 60 {
			t.Errorf("%s geometry %v (%d positions), want 60", can, g, g.Positions())
		}
	}
	for _, can := range []string{"O1", "O2", "O3"} {
		g, ok := DefaultGeometry(can)
		if !ok || g.Positions() != 48 {
			t.Errorf("%s geometry %v (%d positions), want 48", can, g, g.Positions())
		}
	}
	// A3 and A4 are out of commission and must not be offered as walkable.
	for _, can := range []string{"A3", "A4"} {
		if _, ok := DefaultGeometry(can); ok {
			t.Errorf("%s has a geometry, but the can is out of commission", can)
		}
	}
}
