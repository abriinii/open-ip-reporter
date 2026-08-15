package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"openipreporter/internal/capture"
	"openipreporter/internal/export"
	"openipreporter/internal/parse"
	"openipreporter/internal/site"
	"openipreporter/internal/update"
	"openipreporter/internal/walk"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the bridge between the walk engine and the window. Every method here
// is callable from the frontend.
//
// The engine itself knows nothing about this layer, which is deliberate: the
// rules that keep the data correct are testable without a UI in the loop.
type App struct {
	ctx context.Context

	mu      sync.Mutex
	session *walk.Session

	// lastReport collapses the miner's own double-fire. Antminers send every
	// report twice a second apart, so without this every machine would be
	// recorded at two consecutive positions.
	lastReport map[string]time.Time

	// walking is whether captures are being recorded. The session outlives it:
	// stopping ends the walk but keeps the rack in hand so it can be exported.
	walking bool

	// unknown counts datagrams no handler recognised, by port. A miner type we
	// cannot parse yet is otherwise indistinguishable from a miner that never
	// broadcast, which is exactly the dead end Whatsminer produced.
	unknown map[int]int

	layoutErr  string // why the can list fell back to defaults, if it did
	listening  bool
	boundPorts int
	exported   string // filename of the most recent export, for the status bar
	stopListen chan struct{}
	listenDone <-chan struct{}

	sessionDir   string
	cansPath     string
	settingsPath string
	version      string
	updateRepo   string
	latest       *update.Release
	updateState  string
	layout       site.Site
}

func NewApp() *App {
	return &App{
		lastReport:   map[string]time.Time{},
		unknown:      map[int]int{},
		sessionDir:   "sessions",
		cansPath:     "cans.json",
		settingsPath: "settings.json",
		updateRepo:   "abriinii/open-ip-reporter",
		// Seeded so the app is usable before startup has read the file, and so
		// a failure to read it later degrades to the defaults rather than to an
		// empty list with no cans to pick.
		layout: site.Default(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadLayout()
	// Kicked from Go rather than waiting for the window to ask. The result is
	// kept in State, so it is picked up on an ordinary render even if the
	// event never arrives.
	go a.checkForUpdate()
	a.startListening()
}

func (a *App) shutdown(ctx context.Context) {
	a.stopListening()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.save()
}

// --- listening -------------------------------------------------------------

func (a *App) startListening() {
	a.mu.Lock()
	if a.listening {
		a.mu.Unlock()
		return
	}
	a.stopListen = make(chan struct{})
	stop := a.stopListen
	a.mu.Unlock()

	bound, done, err := capture.Listen(capture.DefaultPorts, a.onPacket, stop)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.listening = false
		a.boundPorts = 0
		return
	}
	a.listening = true
	a.boundPorts = bound
	a.listenDone = done
}

func (a *App) stopListening() {
	a.mu.Lock()
	if !a.listening {
		a.mu.Unlock()
		return
	}
	close(a.stopListen)
	a.listening = false
	done := a.listenDone
	a.mu.Unlock()

	// Wait for the sockets to actually close rather than merely asking them
	// to, so nothing is still holding a port after shutdown returns.
	if done != nil {
		<-done
	}
}

// onPacket runs for every datagram received. It is the only path by which a
// machine gets recorded automatically.
func (a *App) onPacket(p capture.Packet) {
	rep, ok := parse.Decode(parse.Packet{
		TS: p.TS, SrcIP: p.SrcIP, SrcPort: p.SrcPort, DstPort: p.DstPort, Data: p.Data,
	})
	if !ok {
		// Not a miner report we understand. Chatter is constant, so only
		// count traffic that looks addressed to a miner tool rather than
		// everything on the wire.
		if interesting(p.DstPort) {
			a.mu.Lock()
			a.unknown[p.DstPort]++
			n := a.unknown[p.DstPort]
			a.mu.Unlock()
			a.emit("notice", fmt.Sprintf(
				"%d packet(s) on udp/%d that no miner handler recognises — send a capture and it can be supported",
				n, p.DstPort))
		}
		return
	}

	a.mu.Lock()
	if a.session == nil || !a.walking {
		a.mu.Unlock()
		return // nothing to record into, or the walk is stopped
	}
	// The miner's own repeat, not a second machine.
	if last, seen := a.lastReport[rep.MAC]; seen && p.TS.Sub(last) <= parse.DoubleFireWindow {
		a.lastReport[rep.MAC] = p.TS
		a.mu.Unlock()
		return
	}
	a.lastReport[rep.MAC] = p.TS

	if a.session.Full() {
		a.mu.Unlock()
		a.emit("notice", "Rack is full — every position is recorded.")
		return
	}

	// A MAC already in this rack is refused, not recorded. The position stays
	// where it is, so the next real machine takes it.
	if _, err := a.session.Record(rep.MAC, rep.IP, rep.Vendor, rep.TS); err != nil {
		var dup *walk.DuplicateError
		if errors.As(err, &dup) {
			a.mu.Unlock()
			a.emit("rejected", fmt.Sprintf("Already recorded at %s", dup.Position.Short()))
			return
		}
		a.mu.Unlock()
		a.emit("notice", err.Error())
		return
	}

	expected := a.session.Can
	// The can is inferred from the source address using one site's addressing
	// scheme. Somewhere else that inference is meaningless, and a name it
	// invents will not be in the can list — so only speak up when the derived
	// can is one this site actually has. Silence beats crying wolf on every
	// single capture at a site the scheme was never written for.
	knownCan := rep.Can != "" && a.layout.Has(rep.Can)
	a.save()
	a.mu.Unlock()

	// A report from a can other than the one being walked is worth saying out
	// loud: either the walk is in the wrong place, or broadcasts are crossing
	// cans and the operator needs to know before trusting the rack.
	if knownCan && rep.Can != expected {
		a.emit("notice", fmt.Sprintf("That machine answered from %s, not %s — check you are in the right can.", rep.Can, expected))
	}
	a.emit("captured", rep.MAC)
}

// interesting reports whether an unrecognised packet on this port is worth
// telling the operator about. The list is the vendor ports we bind on purpose;
// everything else is background noise and would only cry wolf.
func interesting(port int) bool {
	switch port {
	case 14235, 14236, 8888, 8889, 8890, 11503, 18650, 1314, 9999, 12345, 54321, 48899, 9527:
		return true
	}
	return false
}

func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

// --- session lifecycle -----------------------------------------------------

// loadLayout reads the site's can list, falling back to the built-in defaults
// if the file is unreadable so the window still opens and the problem can be
// fixed in the editor rather than in a text file.
func (a *App) loadLayout() {
	a.mu.Lock()
	defer a.mu.Unlock()
	loaded, err := site.Load(a.cansPath)
	if err != nil {
		a.layout = site.Default()
		a.layoutErr = err.Error()
		return
	}
	a.layout = loaded
	a.layoutErr = ""
}

// Layout returns the current can list, for the editor.
func (a *App) Layout() []site.Can {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.layout.Cans) == 0 {
		a.layout = site.Default()
	}
	return a.layout.Cans
}

// SaveLayout replaces the can list. It refuses anything that would not
// describe a real set of racks, and says which entry is wrong.
func (a *App) SaveLayout(cans []site.Can) State {
	next := site.Site{Cans: cans}.Normalise()
	if err := next.Validate(); err != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.stateLocked(err.Error())
	}
	if err := next.Save(a.cansPath); err != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.stateLocked(err.Error())
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.layout = next
	a.layoutErr = ""
	return a.stateLocked("")
}

// settings is the handful of preferences worth remembering between runs.
type settings struct {
	// CheckForUpdates is the only outbound network traffic this program makes,
	// so it gets an explicit off switch rather than being buried.
	CheckForUpdates *bool `json:"checkForUpdates,omitempty"`
}

func (a *App) loadSettings() settings {
	var s settings
	data, err := os.ReadFile(a.settingsPath)
	if err == nil {
		json.Unmarshal(data, &s)
	}
	if s.CheckForUpdates == nil {
		on := true
		s.CheckForUpdates = &on
	}
	return s
}

func (a *App) saveSettings(s settings) {
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		os.WriteFile(a.settingsPath, append(data, '\n'), 0o644)
	}
}

// SetCheckForUpdates turns the version check on or off, and remembers it.
func (a *App) SetCheckForUpdates(on bool) {
	s := a.loadSettings()
	s.CheckForUpdates = &on
	a.saveSettings(s)
}

// CheckForUpdate asks GitHub whether a newer release exists and reports what
// happened, so the window can show that it is checking rather than doing it
// invisibly.
//
// Called by the frontend once its event handlers are registered. Doing it from
// startup instead would race: a fast reply arrives before anything is
// listening and the result is simply lost.
func (a *App) CheckForUpdate() {
	go a.checkForUpdate()
}

// newChecker is a variable so tests can point the check at a stub instead of
// GitHub.
var newChecker = update.NewChecker

func (a *App) checkForUpdate() {
	set := func(state string) {
		a.mu.Lock()
		a.updateState = state
		a.mu.Unlock()
		// Best effort only. The window may not be listening yet, which is
		// exactly why the state above is what actually matters.
		a.emit("update-status", state)
	}

	if on := a.loadSettings().CheckForUpdates; on == nil || !*on {
		set("off")
		return
	}
	if a.version == "" || a.version == "dev" {
		set("dev")
		return
	}

	set("checking")

	rel, err := newChecker(a.updateRepo).Check(context.Background(), a.version)
	switch {
	case err != nil:
		// Being unreachable is the normal case on a miner network, so this is
		// reported plainly rather than as a fault.
		set("unreachable")
	case rel == nil:
		set("current")
	default:
		// Kept either way: the notes for the version already installed are
		// worth being able to read without first falling behind.
		a.mu.Lock()
		a.latest = rel
		a.mu.Unlock()
		if rel.Newer {
			set("available")
		} else {
			set("current")
		}
	}
}

// OpenReleasePage sends the operator to the download page in their browser.
// Nothing is downloaded or installed by this program.
func (a *App) OpenReleasePage() {
	a.mu.Lock()
	rel := a.latest
	a.mu.Unlock()
	if rel != nil && rel.URL != "" {
		runtime.BrowserOpenURL(a.ctx, rel.URL)
	}
}

// Version is the running build, shown next to the update notice.
func (a *App) Version() string { return a.version }

// Cans lists the cans that can be walked, in floor order.
func (a *App) Cans() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.layout.Cans) == 0 {
		a.layout = site.Default()
	}
	return a.layout.Names()
}

// StartSession begins a walk, resuming a saved one for the same rack if there
// is one. Resuming rather than overwriting is the whole point of persistence:
// a closed lid mid-rack must not cost the walk.
func (a *App) StartSession(can string, rack int) State {
	a.mu.Lock()
	defer a.mu.Unlock()

	g, ok := a.layout.Geometry(can)
	if !ok {
		return a.stateLocked(fmt.Sprintf(
			"%q is not in the can list — add it under Cans", can))
	}

	path := a.pathFor(can, rack)
	if existing, err := walk.Load(path); err == nil {
		a.session = existing
		a.walking = true
		a.lastReport = map[string]time.Time{}
		return a.stateLocked("")
	}

	s, err := walk.NewSession(can, rack, g)
	if err != nil {
		return a.stateLocked(err.Error())
	}
	a.session = s
	a.walking = true
	a.lastReport = map[string]time.Time{}
	a.save()
	return a.stateLocked("")
}

// StopSession ends the walk but keeps the rack in hand.
//
// The session deliberately survives: exporting is something you do once the
// walking is finished, and clearing it here would mean stopping and exporting
// were mutually exclusive.
func (a *App) StopSession() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.walking = false
	a.save()
	return a.stateLocked("")
}

func (a *App) pathFor(can string, rack int) string {
	return filepath.Join(a.sessionDir, fmt.Sprintf("%s-rack%d.json", can, rack))
}

func (a *App) save() {
	if a.session == nil {
		return
	}
	a.session.Save(a.pathFor(a.session.Can, a.session.Rack))
}

// --- operations ------------------------------------------------------------

// Skip records an empty position. This is the key pressed most on a walk.
func (a *App) Skip() State { return a.mutate(func(s *walk.Session) error { s.Skip(); return nil }) }

func (a *App) Delete(i int) State {
	return a.mutate(func(s *walk.Session) error { return s.Delete(i) })
}

func (a *App) InsertBlankAbove(i int) State {
	return a.mutate(func(s *walk.Session) error { return s.InsertBlankAbove(i) })
}

func (a *App) SetMAC(i int, mac string) State {
	return a.mutate(func(s *walk.Session) error { return s.SetMAC(i, mac) })
}

// SetNextPosition moves where the next machine will land. This is what the
// position boxes at the top of the window write to.
func (a *App) SetNextPosition(column, row int) State {
	return a.mutate(func(s *walk.Session) error {
		return s.SetNextPosition(walk.Position{Can: s.Can, Rack: s.Rack, Column: column, Row: row})
	})
}

// setClipboard is a variable so the copy logic can be tested without a live
// window: the real function needs a Wails context that only exists at runtime.
var setClipboard = runtime.ClipboardSetText

// CopyIP and CopyMAC put one field of a row on the clipboard.
//
// They read from the session rather than taking a value from the window, so
// what lands on the clipboard is what was recorded, not what happened to be
// rendered — the two could differ if the screen were mid-refresh.
func (a *App) CopyIP(i int) State  { return a.copyField(i, "IP") }
func (a *App) CopyMAC(i int) State { return a.copyField(i, "MAC") }

func (a *App) copyField(i int, field string) State {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return a.stateLocked("no rack loaded")
	}
	if i < 0 || i >= len(a.session.Entries) {
		return a.stateLocked(fmt.Sprintf("no row at %d", i+1))
	}

	e := a.session.Entries[i]
	value := e.MAC
	if field == "IP" {
		value = e.IP
	}
	if value == "" {
		return a.stateLocked(fmt.Sprintf("row %d has no %s to copy", i+1, field))
	}
	if err := setClipboard(a.ctx, value); err != nil {
		return a.stateLocked(fmt.Sprintf("could not copy: %v", err))
	}

	st := a.stateLocked("")
	st.Copied = value
	return st
}

// SetNote attaches free text to a position — why a slot was skipped, a machine
// that would not report, anything worth finding again later.
func (a *App) SetNote(i int, note string) State {
	return a.mutate(func(s *walk.Session) error { return s.SetNote(i, note) })
}

func (a *App) SetPosition(i, column, row int) State {
	return a.mutate(func(s *walk.Session) error {
		return s.SetPosition(i, walk.Position{Can: s.Can, Rack: s.Rack, Column: column, Row: row})
	})
}

func (a *App) Undo() State {
	return a.mutate(func(s *walk.Session) error {
		if !s.Undo() {
			return fmt.Errorf("nothing to undo")
		}
		return nil
	})
}

func (a *App) Redo() State {
	return a.mutate(func(s *walk.Session) error {
		if !s.Redo() {
			return fmt.Errorf("nothing to redo")
		}
		return nil
	})
}

func (a *App) mutate(f func(*walk.Session) error) State {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return a.stateLocked("no session: choose a can and rack first")
	}
	if err := f(a.session); err != nil {
		return a.stateLocked(err.Error())
	}
	a.save()
	return a.stateLocked("")
}

// Export writes the walk to a CSV the operator picks a location for.
//
// One format only: the positional file the existing process already consumes.
func (a *App) Export() State {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return a.State()
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export CSV",
		DefaultFilename: export.PositionalName(s),
		Filters:         []runtime.FileFilter{{DisplayName: "CSV (*.csv)", Pattern: "*.csv"}},
	})
	if err != nil || path == "" {
		return a.State() // cancelled
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := export.WriteFile(path, a.session, export.Positional); err != nil {
		return a.stateLocked(err.Error())
	}
	a.exported = filepath.Base(path)
	return a.stateLocked("")
}

// State returns everything the window needs to draw itself.
func (a *App) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked("")
}

// Row is one line of the on-screen history.
type Row struct {
	Index  int    `json:"index"`
	Kind   string `json:"kind"`
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	Column int    `json:"column"`
	Row    int    `json:"row"`
	Label  string `json:"label"`
	Time   string `json:"time"`
	Note   string `json:"note"`
}

// State is a complete snapshot of the walk for the frontend to render.
type State struct {
	Active        bool   `json:"active"`     // a walk is in progress
	HasSession    bool   `json:"hasSession"` // a rack is loaded, walking or not
	Can           string `json:"can"`
	Rack          int    `json:"rack"`
	Rows          int    `json:"rows"`
	Columns       int    `json:"columns"`
	Positions     int    `json:"positions"`
	Entries       []Row  `json:"entries"`
	NextLabel     string `json:"nextLabel"`
	NextColumn    int    `json:"nextColumn"`
	NextRow       int    `json:"nextRow"`
	Recorded      int    `json:"recorded"`
	Exported      string `json:"exported"`
	Version       string `json:"version"`
	UpdateState   string `json:"updateState"`
	LatestVersion string `json:"latestVersion"`
	LatestNotes   string `json:"latestNotes"`
	Copied        string `json:"copied"` // what just went on the clipboard, for confirmation
	Full          bool   `json:"full"`
	CanUndo       bool   `json:"canUndo"`
	CanRedo       bool   `json:"canRedo"`
	Listening     bool   `json:"listening"`
	BoundPorts    int    `json:"boundPorts"`
	Error         string `json:"error"`
}

func (a *App) stateLocked(errMsg string) State {
	st := State{
		Listening:   a.listening,
		BoundPorts:  a.boundPorts,
		Exported:    a.exported,
		Version:     a.version,
		UpdateState: a.updateState,
		Error:       errMsg,
	}
	// Before the early return on purpose: at launch there is no rack loaded,
	// and that is exactly when the update notice needs its version and notes.
	if a.latest != nil {
		st.LatestVersion = a.latest.Version
		st.LatestNotes = a.latest.Notes
	}
	if a.session == nil {
		return st
	}
	s := a.session

	st.Active = a.walking
	st.HasSession = true
	st.Can = s.Can
	st.Rack = s.Rack
	st.Rows = s.Geom.Rows
	st.Columns = s.Geom.Columns
	st.Positions = s.Geom.Positions()
	st.Full = s.Full()
	st.CanUndo = s.CanUndo()
	st.CanRedo = s.CanRedo()

	st.Entries = make([]Row, 0, len(s.Entries))
	for i, e := range s.Entries {
		p, _ := s.PositionOf(i)
		ts := ""
		if !e.TS.IsZero() {
			ts = e.TS.Format("15:04:05")
		}
		st.Entries = append(st.Entries, Row{
			Index:  i,
			Kind:   string(e.Kind),
			MAC:    e.MAC,
			IP:     e.IP,
			Column: p.Column,
			Row:    p.Row,
			Label:  p.Short(),
			Time:   ts,
			Note:   e.Note,
		})
	}

	for _, e := range s.Entries {
		if e.Kind == walk.Reported {
			st.Recorded++
		}
	}

	if next, ok := s.NextPosition(); ok {
		st.NextLabel = next.Short()
		st.NextColumn = next.Column
		st.NextRow = next.Row
	}
	return st
}
