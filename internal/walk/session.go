package walk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kind distinguishes a captured machine from a deliberately empty slot.
type Kind string

const (
	// Reported is a machine that announced itself.
	Reported Kind = "reported"
	// Skipped is a position with nothing to record: an empty slot, a switch,
	// or a machine that would not report. It still occupies a position, which
	// is what keeps everything after it aligned.
	Skipped Kind = "skipped"
)

// Entry is one position's worth of the walk.
type Entry struct {
	Kind   Kind      `json:"kind"`
	MAC    string    `json:"mac,omitempty"`
	IP     string    `json:"ip,omitempty"`
	Vendor string    `json:"vendor,omitempty"`
	TS     time.Time `json:"ts,omitempty"`
	Note   string    `json:"note,omitempty"`

	// HasAnchor pins this entry to an explicit position rather than letting it
	// be derived from its place in the list. Everything after it continues in
	// walk order from there, which is what "I'm off by one, fix it in place"
	// actually needs: correcting one row corrects the rest of the rack too.
	HasAnchor bool     `json:"has_anchor,omitempty"`
	Anchor    Position `json:"anchor,omitempty"`
}

// Session is one rack being walked.
//
// Positions are derived from an entry's place in the list, not stored on it.
// That is what makes delete-shifts-up and insert-shifts-down fall out for
// free: remove an entry and everything below simply renumbers. Storing a
// position per row and trying to keep them in step by hand is how the
// off-by-one corruption happens in the first place.
type Session struct {
	Can     string    `json:"can"`
	Rack    int       `json:"rack"`
	Geom    Geometry  `json:"geometry"`
	Entries []Entry   `json:"entries"`
	Started time.Time `json:"started"`

	// Pending is where the next entry will land when it has been set by hand.
	// Editing the position boxes on screen sets this; the next capture or skip
	// consumes it and carries on in walk order from there.
	Pending    Position `json:"pending,omitempty"`
	HasPending bool     `json:"has_pending,omitempty"`

	undo []snap
	redo []snap
}

// NewSession starts a walk of one rack.
func NewSession(can string, rack int, g Geometry) (*Session, error) {
	if can == "" {
		return nil, fmt.Errorf("can is required")
	}
	if rack < 1 {
		return nil, fmt.Errorf("rack must be 1 or greater, got %d", rack)
	}
	if !g.Valid() {
		return nil, fmt.Errorf("geometry must have positive rows and columns, got %dx%d", g.Rows, g.Columns)
	}
	return &Session{Can: can, Rack: rack, Geom: g, Started: time.Now()}, nil
}

// PositionOf returns the physical position of entry i.
//
// It walks forward from the nearest anchor at or before i, so a correction
// made partway through a rack carries to everything after it.
func (s *Session) PositionOf(i int) (Position, bool) {
	if i < 0 || i >= len(s.Entries) {
		return Position{}, false
	}
	base, steps := s.anchorFor(i)
	return base.Advance(s.Geom, steps)
}

func (s *Session) anchorFor(i int) (base Position, steps int) {
	for j := i; j >= 0; j-- {
		if s.Entries[j].HasAnchor {
			return s.Entries[j].Anchor, i - j
		}
	}
	start, _ := PositionAt(s.Can, s.Rack, s.Geom, 0)
	return start, i
}

// NextPosition is where the next entry will land.
func (s *Session) NextPosition() (Position, bool) {
	if s.HasPending {
		return s.Pending, true
	}
	if len(s.Entries) == 0 {
		return PositionAt(s.Can, s.Rack, s.Geom, 0)
	}
	last, ok := s.PositionOf(len(s.Entries) - 1)
	if !ok {
		return Position{}, false
	}
	return last.Next(s.Geom)
}

// SetNextPosition moves the walk to a position without touching what has
// already been recorded. This is the "I'm off by one, fix it in place" case:
// correct the boxes on screen and the next machine lands where you say, with
// the rest of the rack following on from there.
func (s *Session) SetNextPosition(p Position) error {
	if !p.Valid(s.Geom) {
		return fmt.Errorf("%s is outside a %d x %d rack", p.Short(), s.Geom.Columns, s.Geom.Rows)
	}
	s.snapshot()
	s.Pending = p
	s.HasPending = true
	return nil
}

// takePending applies a hand-set position to a newly appended entry.
func (s *Session) takePending(e *Entry) {
	if s.HasPending {
		e.HasAnchor = true
		e.Anchor = s.Pending
		s.HasPending = false
		s.Pending = Position{}
	}
}

// Full reports whether the rack has as many entries as it has positions.
func (s *Session) Full() bool { return len(s.Entries) >= s.Geom.Positions() }

// DuplicateError reports an attempt to record a MAC the rack already holds,
// and says where it already is so the operator can go and look.
type DuplicateError struct {
	MAC      string
	Index    int
	Position Position
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("%s is already recorded at %s", e.MAC, e.Position)
}

// Find returns the index of the entry holding a MAC.
func (s *Session) Find(mac string) (int, bool) {
	mac = normaliseMAC(mac)
	if mac == "" {
		return 0, false
	}
	for i, e := range s.Entries {
		if e.Kind == Reported && e.MAC == mac {
			return i, true
		}
	}
	return 0, false
}

// Record appends a captured machine.
//
// A MAC already in this rack is refused outright rather than recorded and
// flagged. A duplicate is never useful data: it is a double press, or a miner
// heard from twice, and writing it would consume a position that belongs to a
// real machine — shifting everything after it by one, which is the exact
// corruption this tool exists to prevent. Refusing costs nothing, because the
// operator can simply press the right miner next.
func (s *Session) Record(mac, ip, vendor string, ts time.Time) (Position, error) {
	if i, found := s.Find(mac); found {
		at, _ := s.PositionOf(i)
		return Position{}, &DuplicateError{MAC: normaliseMAC(mac), Index: i, Position: at}
	}
	s.snapshot()
	e := Entry{Kind: Reported, MAC: normaliseMAC(mac), IP: ip, Vendor: vendor, TS: ts}
	s.takePending(&e)
	s.Entries = append(s.Entries, e)
	p, _ := s.PositionOf(len(s.Entries) - 1)
	return p, nil
}

// Skip records a deliberately empty position: an empty slot, a switch, or a
// machine that will not report. This is the single most-pressed key on a walk.
func (s *Session) Skip() (Position, bool) {
	s.snapshot()
	e := Entry{Kind: Skipped, TS: time.Now()}
	s.takePending(&e)
	s.Entries = append(s.Entries, e)
	return s.PositionOf(len(s.Entries) - 1)
}

// Delete removes an entry. Everything below shifts up one position.
func (s *Session) Delete(i int) error {
	if i < 0 || i >= len(s.Entries) {
		return fmt.Errorf("no entry at %d", i)
	}
	s.snapshot()
	s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
	return nil
}

// InsertBlankAbove puts a skipped position at i. Everything below shifts down
// one position. This is the fix for a machine that was missed on the way past.
func (s *Session) InsertBlankAbove(i int) error {
	if i < 0 || i > len(s.Entries) {
		return fmt.Errorf("cannot insert at %d", i)
	}
	s.snapshot()
	s.Entries = append(s.Entries, Entry{})
	copy(s.Entries[i+1:], s.Entries[i:])
	s.Entries[i] = Entry{Kind: Skipped, TS: time.Now()}
	return nil
}

// SetMAC edits an entry's MAC by hand, for a machine read off its web UI
// rather than captured. Setting a MAC turns a skipped position into a
// reported one; clearing it does the reverse.
func (s *Session) SetMAC(i int, mac string) error {
	if i < 0 || i >= len(s.Entries) {
		return fmt.Errorf("no entry at %d", i)
	}
	mac = strings.TrimSpace(mac)
	if mac != "" && !validMAC(mac) {
		return fmt.Errorf("%q is not a MAC address", mac)
	}
	// Same rule as Record: one MAC, one position. Typing in a MAC that is
	// already somewhere in the rack is the same mistake as capturing it twice.
	if j, found := s.Find(mac); found && j != i {
		at, _ := s.PositionOf(j)
		return &DuplicateError{MAC: normaliseMAC(mac), Index: j, Position: at}
	}
	s.snapshot()
	s.Entries[i].MAC = normaliseMAC(mac)
	if mac == "" {
		s.Entries[i].Kind = Skipped
	} else {
		s.Entries[i].Kind = Reported
	}
	return nil
}

// SetPosition pins an entry to a position. Everything after it renumbers to
// follow, so correcting one row corrects the remainder of the rack.
func (s *Session) SetPosition(i int, p Position) error {
	if i < 0 || i >= len(s.Entries) {
		return fmt.Errorf("no entry at %d", i)
	}
	if !p.Valid(s.Geom) {
		return fmt.Errorf("%s is outside a %dx%d rack", p, s.Geom.Columns, s.Geom.Rows)
	}
	s.snapshot()
	s.Entries[i].HasAnchor = true
	s.Entries[i].Anchor = p
	return nil
}

// ClearPosition removes a pin, letting the entry derive its position again.
func (s *Session) ClearPosition(i int) error {
	if i < 0 || i >= len(s.Entries) {
		return fmt.Errorf("no entry at %d", i)
	}
	s.snapshot()
	s.Entries[i].HasAnchor = false
	s.Entries[i].Anchor = Position{}
	return nil
}

// Duplicates returns MACs that appear at more than one position, with the
// indices they appear at. On this hardware a miner sends every report twice a
// second apart, so the capture layer collapses that pair before it ever
// reaches a session; anything reaching here is a genuine repeat.
func (s *Session) Duplicates() map[string][]int {
	byMAC := map[string][]int{}
	for i, e := range s.Entries {
		if e.Kind == Reported && e.MAC != "" {
			byMAC[e.MAC] = append(byMAC[e.MAC], i)
		}
	}
	for mac, idx := range byMAC {
		if len(idx) < 2 {
			delete(byMAC, mac)
		}
	}
	return byMAC
}

// SortedDuplicateMACs is Duplicates in a stable order, for display.
func (s *Session) SortedDuplicateMACs() []string {
	d := s.Duplicates()
	macs := make([]string, 0, len(d))
	for m := range d {
		macs = append(macs, m)
	}
	sort.Strings(macs)
	return macs
}

// --- undo / redo -----------------------------------------------------------
//
// Snapshots rather than an operation log. A rack is at most sixty entries, so
// copying the list is trivially cheap, and it cannot drift out of step with
// the real state the way inverse operations can.

const maxUndo = 200

type snap struct {
	entries    []Entry
	pending    Position
	hasPending bool
}

// capture takes a copy of everything an operation could change. The pending
// position belongs in here as much as the entries do: undoing a correction has
// to put the walk back where it was pointing, not just restore the rows.
func (s *Session) capture() snap {
	entries := make([]Entry, len(s.Entries))
	copy(entries, s.Entries)
	return snap{entries: entries, pending: s.Pending, hasPending: s.HasPending}
}

func (s *Session) restore(sn snap) {
	s.Entries = sn.entries
	s.Pending = sn.pending
	s.HasPending = sn.hasPending
}

func (s *Session) snapshot() {
	s.undo = append(s.undo, s.capture())
	if len(s.undo) > maxUndo {
		s.undo = s.undo[len(s.undo)-maxUndo:]
	}
	s.redo = nil
}

// Undo steps back one operation. It reports false when there is nothing to undo.
func (s *Session) Undo() bool {
	if len(s.undo) == 0 {
		return false
	}
	s.redo = append(s.redo, s.capture())
	s.restore(s.undo[len(s.undo)-1])
	s.undo = s.undo[:len(s.undo)-1]
	return true
}

// Redo steps forward again. It reports false when there is nothing to redo.
func (s *Session) Redo() bool {
	if len(s.redo) == 0 {
		return false
	}
	s.undo = append(s.undo, s.capture())
	s.restore(s.redo[len(s.redo)-1])
	s.redo = s.redo[:len(s.redo)-1]
	return true
}

// CanUndo and CanRedo let an interface grey out what is unavailable.
func (s *Session) CanUndo() bool { return len(s.undo) > 0 }
func (s *Session) CanRedo() bool { return len(s.redo) > 0 }

// --- persistence -----------------------------------------------------------

// Save writes the session to disk. The write is atomic: a crash or a lid close
// mid-write leaves the previous good file in place rather than a truncated
// one, because losing a rack to a half-written file would be its own kind of
// silent data loss.
func (s *Session) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Load reads a session back. Undo history is deliberately not persisted: it
// describes a working state, not the walk itself.
func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !s.Geom.Valid() {
		return nil, fmt.Errorf("%s: geometry is %dx%d, which is not a rack", path, s.Geom.Rows, s.Geom.Columns)
	}
	return &s, nil
}
