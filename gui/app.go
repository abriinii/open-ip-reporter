package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"openipreporter/internal/capture"
	"openipreporter/internal/export"
	"openipreporter/internal/parse"
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

	listening  bool
	boundPorts int
	exported   string // filename of the most recent export, for the status bar
	stopListen chan struct{}
	listenDone <-chan struct{}

	sessionDir string
}

func NewApp() *App {
	return &App{
		lastReport: map[string]time.Time{},
		unknown:    map[int]int{},
		sessionDir: "sessions",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
	a.save()
	a.mu.Unlock()

	// A report from a can other than the one being walked is worth saying out
	// loud: either the walk is in the wrong place, or broadcasts are crossing
	// cans and the operator needs to know before trusting the rack.
	if rep.Can != "" && rep.Can != expected {
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

// Cans lists the cans that can be walked, in floor order.
func (a *App) Cans() []string {
	cans := []string{"A1", "A2", "A5", "A6", "A7", "A8", "B1", "B2", "B3", "B4", "O1", "O2", "O3"}
	sort.SliceStable(cans, func(i, j int) bool {
		if cans[i][0] != cans[j][0] {
			return cans[i][0] < cans[j][0]
		}
		return cans[i][1:] < cans[j][1:]
	})
	return cans
}

// StartSession begins a walk, resuming a saved one for the same rack if there
// is one. Resuming rather than overwriting is the whole point of persistence:
// a closed lid mid-rack must not cost the walk.
func (a *App) StartSession(can string, rack int) State {
	a.mu.Lock()
	defer a.mu.Unlock()

	g, ok := walk.DefaultGeometry(can)
	if !ok {
		return a.stateLocked(fmt.Sprintf("%s is not a can that can be walked", can))
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
	Active     bool   `json:"active"`     // a walk is in progress
	HasSession bool   `json:"hasSession"` // a rack is loaded, walking or not
	Can        string `json:"can"`
	Rack       int    `json:"rack"`
	Rows       int    `json:"rows"`
	Columns    int    `json:"columns"`
	Positions  int    `json:"positions"`
	Entries    []Row  `json:"entries"`
	NextLabel  string `json:"nextLabel"`
	NextColumn int    `json:"nextColumn"`
	NextRow    int    `json:"nextRow"`
	Recorded   int    `json:"recorded"`
	Exported   string `json:"exported"`
	Copied     string `json:"copied"` // what just went on the clipboard, for confirmation
	Full       bool   `json:"full"`
	CanUndo    bool   `json:"canUndo"`
	CanRedo    bool   `json:"canRedo"`
	Listening  bool   `json:"listening"`
	BoundPorts int    `json:"boundPorts"`
	Error      string `json:"error"`
}

func (a *App) stateLocked(errMsg string) State {
	st := State{
		Listening:  a.listening,
		BoundPorts: a.boundPorts,
		Exported:   a.exported,
		Error:      errMsg,
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
