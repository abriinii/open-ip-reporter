package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openipreporter/internal/capture"
	"openipreporter/internal/export"
	"openipreporter/internal/site"
	"openipreporter/internal/update"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	dir := t.TempDir()
	a.sessionDir = dir
	// Never let a test write a can list into the working copy.
	a.cansPath = filepath.Join(dir, "cans.json")
	a.settingsPath = filepath.Join(dir, "settings.json")
	// A fresh install has no cans. These tests are about walking one, so they
	// get a layout to walk.
	a.layout = site.Example()
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

// Copying reads from the session rather than from whatever the window last
// rendered, so what lands on the clipboard is what was actually recorded.
func TestCopyIPAndMAC(t *testing.T) {
	var clipboard string
	orig := setClipboard
	setClipboard = func(_ context.Context, s string) error { clipboard = s; return nil }
	defer func() { setClipboard = orig }()

	a := newTestApp(t)
	a.StartSession("B2", 1)
	a.onPacket(antminerPacket("22.1.1.10", "02:81:f5:ea:e1:db", time.Now()))
	a.Skip()

	if st := a.CopyIP(0); st.Error != "" || st.Copied != "22.1.1.10" {
		t.Errorf("CopyIP: err=%q copied=%q", st.Error, st.Copied)
	}
	if clipboard != "22.1.1.10" {
		t.Errorf("clipboard holds %q, want the IP", clipboard)
	}

	if st := a.CopyMAC(0); st.Error != "" || st.Copied != "02:81:f5:ea:e1:db" {
		t.Errorf("CopyMAC: err=%q copied=%q", st.Error, st.Copied)
	}
	if clipboard != "02:81:f5:ea:e1:db" {
		t.Errorf("clipboard holds %q, want the MAC", clipboard)
	}
}

// A skipped row has nothing to copy, and a row that does not exist is not a
// crash. Both must report rather than silently putting something wrong on the
// clipboard.
func TestCopyRefusesWhenThereIsNothingToCopy(t *testing.T) {
	clipboard := "untouched"
	orig := setClipboard
	setClipboard = func(_ context.Context, s string) error { clipboard = s; return nil }
	defer func() { setClipboard = orig }()

	a := newTestApp(t)
	a.StartSession("B2", 1)
	a.Skip()

	for _, st := range []State{a.CopyIP(0), a.CopyMAC(0), a.CopyIP(99), a.CopyMAC(-1)} {
		if st.Error == "" {
			t.Errorf("copy succeeded with nothing to copy: %+v", st.Copied)
		}
	}
	if clipboard != "untouched" {
		t.Errorf("clipboard was written as %q despite there being nothing to copy", clipboard)
	}
}

// The can list is what makes this usable at a second site.
func TestCanListDrivesWhatCanBeWalked(t *testing.T) {
	a := newTestApp(t)
	a.cansPath = filepath.Join(t.TempDir(), "cans.json")
	a.loadLayout()

	// A site with entirely different names and a rack bigger than any here.
	st := a.SaveLayout([]site.Can{
		{Name: "Shed", Rows: 12, Columns: 8},
		{Name: "Barn", Rows: 4, Columns: 3},
	})
	if st.Error != "" {
		t.Fatalf("SaveLayout: %s", st.Error)
	}
	if got := a.Cans(); len(got) != 2 || got[0] != "Barn" || got[1] != "Shed" {
		t.Errorf("Cans() = %v, want [Barn Shed]", got)
	}

	st = a.StartSession("Shed", 1)
	if st.Error != "" || !st.Active {
		t.Fatalf("could not walk a foreign can: %q", st.Error)
	}
	if st.Positions != 96 {
		t.Errorf("Shed has %d positions, want 96", st.Positions)
	}

	// And the old site's cans are gone.
	if st := a.StartSession("B1", 1); st.Error == "" {
		t.Error("walked a can that is not in the list")
	}
}

func TestSaveLayoutRefusesNonsenseAndKeepsTheOldList(t *testing.T) {
	a := newTestApp(t)
	a.cansPath = filepath.Join(t.TempDir(), "cans.json")
	a.loadLayout()
	before := len(a.Cans())

	if st := a.SaveLayout([]site.Can{{Name: "Broken", Rows: 0, Columns: 5}}); st.Error == "" {
		t.Error("saved a can with no rows")
	}
	if got := len(a.Cans()); got != before {
		t.Errorf("a rejected save changed the list: %d cans, want %d", got, before)
	}
}

// At a site whose addressing does not match the scheme, the inferred can is
// meaningless and must not produce a warning on every capture.
func TestNoWrongCanWarningForCansThisSiteDoesNotHave(t *testing.T) {
	a := newTestApp(t)
	a.cansPath = filepath.Join(t.TempDir(), "cans.json")
	a.loadLayout()
	a.SaveLayout([]site.Can{{Name: "Shed", Rows: 12, Columns: 8}})
	a.StartSession("Shed", 1)

	// 22.x derives as "B2", which this site has never heard of.
	a.onPacket(antminerPacket("22.1.1.10", "02:81:f5:ea:e1:db", time.Now()))

	if n := len(a.State().Entries); n != 1 {
		t.Fatalf("recorded %d rows, want 1", n)
	}
	if a.State().Error != "" {
		t.Errorf("warned about a can this site does not have: %q", a.State().Error)
	}
}

// The update check is the only outbound traffic in the program, so the off
// switch has to actually stop it.
func TestUpdateCheckRespectsTheOffSwitch(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer srv.Close()

	a.version = "v1.0.0"
	a.SetCheckForUpdates(false)
	a.checkForUpdate()
	if called {
		t.Error("checked for updates after being told not to")
	}

	// And back on again.
	a.SetCheckForUpdates(true)
	if on := a.loadSettings().CheckForUpdates; on == nil || !*on {
		t.Error("could not turn the check back on")
	}
}

// Default on, since that is what was asked for, but only after a real decision
// rather than by accident of a missing file.
func TestUpdateCheckIsOnByDefault(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	if on := a.loadSettings().CheckForUpdates; on == nil || !*on {
		t.Error("a fresh install has the update check off")
	}
}

// A dev build must not phone home at all.
func TestDevBuildDoesNotCheck(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.version = "dev"
	a.updateRepo = "x/y"
	a.checkForUpdate() // would panic on a nil ctx if it tried to emit
	if a.latest != nil {
		t.Error("a dev build reported an update")
	}
}

// The update status has to survive on State alone. Relying on the event is
// what shipped twice as a feature that silently did nothing, because a
// dropped event leaves no trace anywhere.
func TestUpdateStatusIsVisibleInStateWithoutAnyEvent(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.ctx = nil // no window: every emit is a no-op, as on a real launch race

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v9.9.9","body":"Notes here.","html_url":"https://example.test"}`))
	}))
	defer srv.Close()

	a.version = "v1.0.0"
	a.updateRepo = "x/y"
	// Point the checker at the stub rather than GitHub.
	origNew := newChecker
	newChecker = func(repo string) *update.Checker {
		c := update.NewChecker(repo)
		c.BaseURL = srv.URL
		c.Client = srv.Client()
		return c
	}
	defer func() { newChecker = origNew }()

	a.checkForUpdate()

	st := a.State()
	if st.UpdateState != "available" {
		t.Fatalf("UpdateState = %q, want \"available\" — the window has no other way to know", st.UpdateState)
	}
	if st.LatestVersion != "v9.9.9" || st.LatestNotes != "Notes here." {
		t.Errorf("State carries %q / %q", st.LatestVersion, st.LatestNotes)
	}
}

func TestUpdateStateReportsBeingOffline(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.version = "v1.0.0"
	a.updateRepo = "x/y"
	origNew := newChecker
	newChecker = func(repo string) *update.Checker {
		c := update.NewChecker(repo)
		c.BaseURL = "http://127.0.0.1:1" // nothing listening
		c.Client = &http.Client{Timeout: time.Second}
		return c
	}
	defer func() { newChecker = origNew }()

	a.checkForUpdate()
	if got := a.State().UpdateState; got != "unreachable" {
		t.Errorf("UpdateState = %q, want \"unreachable\"", got)
	}
}

// Nothing may be written next to the executable. Running from Downloads used
// to scatter files there, and the sessions folder holds walks in progress —
// tidying it away loses real work.
func TestNothingIsWrittenToTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	a.dataDir = dir
	a.sessionDir = filepath.Join(dir, "sessions")
	a.cansPath = filepath.Join(dir, "cans.json")
	a.settingsPath = filepath.Join(dir, "settings.json")

	a.loadLayout()
	a.SaveLayout([]site.Can{{Name: "B1", Rows: 10, Columns: 5}})
	a.SetCheckForUpdates(false)
	a.StartSession("B1", 1)
	a.onPacket(antminerPacket("21.1.1.43", "02:81:f5:ea:e1:db", time.Now()))

	for _, stray := range []string{"cans.json", "settings.json", "sessions"} {
		if _, err := os.Stat(stray); err == nil {
			os.RemoveAll(stray)
			t.Errorf("%s was written beside the executable", stray)
		}
	}
	for _, want := range []string{"cans.json", "settings.json", "sessions"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s missing from the data folder: %v", want, err)
		}
	}
}

// Upgrading must not look like the can list and an unfinished walk vanished.
func TestFilesFromOlderVersionsAreMovedAcross(t *testing.T) {
	old := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(old); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	os.WriteFile("cans.json", []byte(`{"cans":[{"name":"Shed","rows":4,"columns":3}]}`), 0o644)
	os.MkdirAll("sessions", 0o755)
	os.WriteFile(filepath.Join("sessions", "Shed-rack1.json"), []byte("{}"), 0o644)

	dst := t.TempDir()
	migrateFromWorkingDir(dst)

	for _, name := range []string{"cans.json", "sessions"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s was not carried across: %v", name, err)
		}
		if _, err := os.Stat(name); err == nil {
			t.Errorf("%s was left behind as well", name)
		}
	}
}

// A file already in the data folder must win; the stale copy beside the
// executable is the older one.
func TestMigrationNeverOverwrites(t *testing.T) {
	old := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(old)
	defer os.Chdir(cwd)

	os.WriteFile("cans.json", []byte(`{"cans":[{"name":"Stale","rows":1,"columns":1}]}`), 0o644)

	dst := t.TempDir()
	keep := []byte(`{"cans":[{"name":"Current","rows":10,"columns":5}]}`)
	os.WriteFile(filepath.Join(dst, "cans.json"), keep, 0o644)

	migrateFromWorkingDir(dst)

	got, _ := os.ReadFile(filepath.Join(dst, "cans.json"))
	if string(got) != string(keep) {
		t.Errorf("the current can list was overwritten by an older one: %s", got)
	}
}

// CheckForUpdate must return a finished result, not kick something off and
// leave the window to work out when it landed. Every fault in this feature
// came from the answer existing before or after whoever was looking for it.
func TestCheckForUpdateReturnsTheFinishedResult(t *testing.T) {
	a := newTestApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.version = "v1.0.0"
	a.updateRepo = "x/y"

	// A deliberately slow reply, well past any polling window.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{"tag_name":"v9.9.9","body":"Notes.","html_url":"https://example.test"}`))
	}))
	defer srv.Close()

	orig := newChecker
	newChecker = func(repo string) *update.Checker {
		c := update.NewChecker(repo)
		c.BaseURL = srv.URL
		c.Client = srv.Client()
		return c
	}
	defer func() { newChecker = orig }()

	st := a.CheckForUpdate()

	if st.UpdateState != "available" {
		t.Fatalf("UpdateState = %q, want available on return", st.UpdateState)
	}
	if st.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion = %q on return; the window would have nothing to open", st.LatestVersion)
	}
	if st.LatestNotes != "Notes." {
		t.Errorf("LatestNotes = %q on return", st.LatestNotes)
	}
}
