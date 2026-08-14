// Package export writes a finished walk out as CSV.
//
// One format, deliberately: what other people on site already consume. Row n
// of the file is grid position n, and a skipped position is a blank row, so
// nothing about their workflow changes.
//
// The position recorded during the walk is what makes those blank rows land in
// the right places — including gaps left by correcting the walk mid-rack, which
// a person pressing Skip would have no way to reproduce.
package export

import (
	"betteripreporter/internal/walk"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Positional writes the format the existing process expects: one row per grid
// position, in walk order, with a blank row wherever nothing was recorded.
//
// Rows are placed by their actual position rather than by their order in the
// list. That matters when the operator has jumped the walk forward: the gap
// has to become blank rows, or every row after it is off by the size of the
// jump — which is the exact failure this project exists to prevent.
func Positional(w io.Writer, s *walk.Session) error {
	rows, err := layout(s)
	if err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	for _, e := range rows {
		if e == nil || e.Kind != walk.Reported {
			// A blank row holds the position open, exactly as the Skip button
			// does in the tool this replaces.
			if err := cw.Write([]string{"", ""}); err != nil {
				return err
			}
			continue
		}
		if err := cw.Write([]string{e.IP, e.MAC}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// layout places entries at their real position index, leaving nil in the gaps.
// The result is as long as the last occupied position, so a rack walked only
// halfway exports short rather than being padded out to a full rack — a short
// rack should read as short and be walked again, not be quietly completed.
func layout(s *walk.Session) ([]*walk.Entry, error) {
	if len(s.Entries) == 0 {
		return nil, nil
	}

	placed := map[int]*walk.Entry{}
	last := -1
	for i := range s.Entries {
		p, ok := s.PositionOf(i)
		if !ok {
			continue
		}
		idx := p.Index(s.Geom)
		if prev, taken := placed[idx]; taken {
			return nil, fmt.Errorf(
				"two entries claim %s: %q and %q — fix the positions before exporting",
				p.Short(), prev.MAC, s.Entries[i].MAC)
		}
		placed[idx] = &s.Entries[i]
		if idx > last {
			last = idx
		}
	}

	rows := make([]*walk.Entry, last+1)
	for idx, e := range placed {
		rows[idx] = e
	}
	return rows, nil
}

// PositionalName is the filename the positional export defaults to: one file
// per rack, which is how the existing process consumes them.
func PositionalName(s *walk.Session) string {
	return fmt.Sprintf("%s-rack%d.csv", s.Can, s.Rack)
}

// WriteFile writes one of the formats to a path, atomically so an interrupted
// write cannot leave a half-file that looks complete.
func WriteFile(path string, s *walk.Session, format func(io.Writer, *walk.Session) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".export-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := format(tmp, s); err != nil {
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
