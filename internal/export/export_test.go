package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"betteripreporter/internal/walk"
)

var b1 = walk.Geometry{Rows: 10, Columns: 6}

func newSession(t *testing.T) *walk.Session {
	t.Helper()
	s, err := walk.NewSession("B1", 3, b1)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func positional(t *testing.T, s *walk.Session) string {
	t.Helper()
	var sb strings.Builder
	if err := Positional(&sb, s); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// Row n of the file is grid position n. A skip is a blank row, which is what
// holds every following row in alignment.
func TestPositionalKeepsSkipsAsBlankRows(t *testing.T) {
	s := newSession(t)
	s.Record("02:81:f5:ea:e1:db", "21.1.1.43", "Antminer", time.Now())
	s.Skip()
	s.Record("02:ad:af:02:ff:45", "21.1.11.232", "Antminer", time.Now())

	want := "21.1.1.43,02:81:f5:ea:e1:db\n" +
		",\n" +
		"21.1.11.232,02:ad:af:02:ff:45\n"
	if got := positional(t, s); got != want {
		t.Errorf("positional export:\n%q\nwant:\n%q", got, want)
	}
}

// The case that makes this worth writing carefully: jumping the walk forward
// leaves real positions empty, and those have to become blank rows or every
// row after the jump is offset by the size of the jump.
func TestPositionalFillsAGapLeftByJumpingTheWalk(t *testing.T) {
	s := newSession(t)
	s.Record("02:00:00:00:00:01", "21.1.1.1", "Antminer", time.Now())

	// Operator realises they are actually at row 5 and corrects the boxes.
	if err := s.SetNextPosition(walk.Position{Can: "B1", Rack: 3, Column: 1, Row: 5}); err != nil {
		t.Fatal(err)
	}
	s.Record("02:00:00:00:00:02", "21.1.1.2", "Antminer", time.Now())

	lines := strings.Split(strings.TrimRight(positional(t, s), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("export has %d rows, want 5 (positions 1..5)\n%v", len(lines), lines)
	}
	if lines[0] != "21.1.1.1,02:00:00:00:00:01" {
		t.Errorf("row 1 = %q", lines[0])
	}
	for _, i := range []int{1, 2, 3} {
		if lines[i] != "," {
			t.Errorf("row %d = %q, want a blank row holding the gap", i+1, lines[i])
		}
	}
	if lines[4] != "21.1.1.2,02:00:00:00:00:02" {
		t.Errorf("row 5 = %q, want the second machine at position 5", lines[4])
	}
}

// A rack walked halfway exports halfway. Padding it out to a full rack would
// make an unfinished rack look finished.
func TestPositionalDoesNotPadToRackSize(t *testing.T) {
	s := newSession(t)
	s.Record("02:00:00:00:00:01", "21.1.1.1", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "21.1.1.2", "Antminer", time.Now())

	if n := strings.Count(positional(t, s), "\n"); n != 2 {
		t.Errorf("export has %d rows, want 2 — a short rack must read as short", n)
	}
}

func TestPositionalOfAnEmptyWalkIsEmpty(t *testing.T) {
	if got := positional(t, newSession(t)); got != "" {
		t.Errorf("empty walk exported %q, want nothing", got)
	}
}

// Two entries at one position would silently drop one. Refuse instead.
func TestPositionalRefusesCollidingPositions(t *testing.T) {
	s := newSession(t)
	s.Record("02:00:00:00:00:01", "21.1.1.1", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "21.1.1.2", "Antminer", time.Now())
	// Pin the second entry back on top of the first.
	if err := s.SetPosition(1, walk.Position{Can: "B1", Rack: 3, Column: 1, Row: 1}); err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	err := Positional(&sb, s)
	if err == nil {
		t.Fatal("export succeeded with two entries at one position, want refusal")
	}
	if !strings.Contains(err.Error(), "C1/1") {
		t.Errorf("error %q does not say which position collided", err)
	}
}

func TestTaggedCarriesPositionOnEveryRow(t *testing.T) {
	s := newSession(t)
	s.Record("02:81:f5:ea:e1:db", "21.1.1.43", "Antminer", time.Now())
	s.Skip()
	s.Record("02:ad:af:02:ff:45", "21.1.11.232", "Antminer", time.Now())

	var sb strings.Builder
	if err := Tagged(&sb, s); err != nil {
		t.Fatal(err)
	}
	want := "can,rack,row,column,ip,mac\n" +
		"B1,3,1,1,21.1.1.43,02:81:f5:ea:e1:db\n" +
		"B1,3,3,1,21.1.11.232,02:ad:af:02:ff:45\n"
	if sb.String() != want {
		t.Errorf("tagged export:\n%q\nwant:\n%q", sb.String(), want)
	}
}

// The tagged file is a join table, so a skipped position has nothing to
// contribute and must not appear as a machine with no MAC.
func TestTaggedOmitsSkippedPositions(t *testing.T) {
	s := newSession(t)
	s.Skip()
	s.Skip()
	var sb strings.Builder
	if err := Tagged(&sb, s); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(sb.String(), "\n"); n != 1 {
		t.Errorf("tagged export has %d lines, want just the header", n)
	}
}

func TestNamesAreOneFilePerRack(t *testing.T) {
	s := newSession(t)
	if got := PositionalName(s); got != "B1-rack3.csv" {
		t.Errorf("PositionalName = %q", got)
	}
	if got := TaggedName(s); got != "B1-rack3-tagged.csv" {
		t.Errorf("TaggedName = %q", got)
	}
}

func TestWriteFileIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	s := newSession(t)
	s.Record("02:81:f5:ea:e1:db", "21.1.1.43", "Antminer", time.Now())

	if err := WriteFile(path, s, Positional); err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only the export", entries)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "21.1.1.43,02:81:f5:ea:e1:db\n" {
		t.Errorf("file holds %q", data)
	}
}

// A failed export must not replace a good file with a broken one.
func TestWriteFileLeavesTheOldFileWhenExportFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	os.WriteFile(path, []byte("previous good export\n"), 0o644)

	s := newSession(t)
	s.Record("02:00:00:00:00:01", "21.1.1.1", "Antminer", time.Now())
	s.Record("02:00:00:00:00:02", "21.1.1.2", "Antminer", time.Now())
	s.SetPosition(1, walk.Position{Can: "B1", Rack: 3, Column: 1, Row: 1}) // collision

	if err := WriteFile(path, s, Positional); err == nil {
		t.Fatal("export succeeded despite a position collision")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "previous good export\n" {
		t.Errorf("previous export was clobbered: %q", data)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only the untouched export", entries)
	}
}
