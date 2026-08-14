package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"betteripreporter/internal/capture"
	"betteripreporter/internal/parse"
	"betteripreporter/internal/walk"

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

	listening  bool
	boundPorts int
	stopListen chan struct{}

	sessionDir string
}

func NewApp() *App {
	return &App{
		lastReport: map[string]time.Time{},
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

	bound, err := capture.Listen(capture.DefaultPorts, a.onPacket, stop)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.listening = false
		a.boundPorts = 0
		return
	}
	a.listening = true
	a.boundPorts = bound
}

func (a *App) stopListening() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.listening {
		return
	}
	close(a.stopListen)
	a.listening = false
}

// onPacket runs for every datagram received. It is the only path by which a
// machine gets recorded automatically.
func (a *App) onPacket(p capture.Packet) {
	rep, ok := parse.Decode(parse.Packet{
		TS: p.TS, SrcIP: p.SrcIP, SrcPort: p.SrcPort, DstPort: p.DstPort, Data: p.Data,
	})
	if !ok {
		return
	}

	a.mu.Lock()
	if a.session == nil {
		a.mu.Unlock()
		return // nothing to record into yet
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

func (a *App) emit(event string, data ...any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

// --- session lifecycle -----------------------------------------------------

// Cans lists the cans that can be walked, in floor order.
func (a *App) Cans() []string {
	cans := []string{"A1", "A2", "A5", "A6", "A7", "A8", "B1", "B2", "B3", "B4", "O1", "O2", "O3", "O4"}
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
		return a.stateLocked(fmt.Sprintf("no geometry known for can %q", can))
	}

	path := a.pathFor(can, rack)
	if existing, err := walk.Load(path); err == nil {
		a.session = existing
		a.lastReport = map[string]time.Time{}
		return a.stateLocked("")
	}

	s, err := walk.NewSession(can, rack, g)
	if err != nil {
		return a.stateLocked(err.Error())
	}
	a.session = s
	a.lastReport = map[string]time.Time{}
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
}

// State is a complete snapshot of the walk for the frontend to render.
type State struct {
	Active     bool   `json:"active"`
	Can        string `json:"can"`
	Rack       int    `json:"rack"`
	Rows       int    `json:"rows"`
	Columns    int    `json:"columns"`
	Positions  int    `json:"positions"`
	Entries    []Row  `json:"entries"`
	NextLabel  string `json:"nextLabel"`
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
		Error:      errMsg,
	}
	if a.session == nil {
		return st
	}
	s := a.session

	st.Active = true
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
		})
	}

	if next, ok := s.NextPosition(); ok {
		st.NextLabel = next.Short()
	}
	return st
}
