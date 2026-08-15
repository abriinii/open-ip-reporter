// Package site holds the layout of the place the tool is being used: which
// cans exist and what shape their racks are.
//
// This is the only part of the program that knows anything site-specific.
// Everything else takes a geometry and works the same anywhere, which is what
// makes the tool usable at a second site without a recompile.
package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"openipreporter/internal/walk"
)

// maxPositions is a sanity ceiling, not a site limit. It exists so a typo
// cannot create a rack with a hundred thousand positions; it is deliberately
// far above any real rack rather than tuned to one site's racks.
const maxPositions = 2000

// Can is one container and the shape of the racks inside it.
type Can struct {
	Name    string `json:"name"`
	Rows    int    `json:"rows"`
	Columns int    `json:"columns"`
}

// Site is the full list, in the order it will be offered.
type Site struct {
	Cans []Can `json:"cans"`
}

// Empty is what a fresh install starts with: no cans at all.
//
// Shipping one site's layout as the default meant every new install began by
// deleting a list of containers that meant nothing to it. An empty list makes
// the first step obvious instead.
func Empty() Site { return Site{} }

// Example is a layout offered as a starting point, never applied on its own.
func Example() Site {
	return Site{Cans: []Can{
		{Name: "A1", Rows: 10, Columns: 5},
		{Name: "A2", Rows: 10, Columns: 5},
		{Name: "A5", Rows: 10, Columns: 5},
		{Name: "A6", Rows: 10, Columns: 5},
		{Name: "A7", Rows: 10, Columns: 5},
		{Name: "A8", Rows: 10, Columns: 5},
		{Name: "B1", Rows: 10, Columns: 5},
		{Name: "B2", Rows: 10, Columns: 5},
		{Name: "B3", Rows: 10, Columns: 5},
		{Name: "B4", Rows: 10, Columns: 5},
		{Name: "O1", Rows: 8, Columns: 6},
		{Name: "O2", Rows: 8, Columns: 6},
		{Name: "O3", Rows: 8, Columns: 6},
	}}
}

// Names lists the can names in order, for a dropdown.
func (s Site) Names() []string {
	out := make([]string, 0, len(s.Cans))
	for _, c := range s.Cans {
		out = append(out, c.Name)
	}
	return out
}

// Geometry returns the rack shape for a can. The second result is false for a
// name the site does not have, so a caller can refuse rather than guess.
func (s Site) Geometry(name string) (walk.Geometry, bool) {
	for _, c := range s.Cans {
		if strings.EqualFold(c.Name, name) {
			return walk.Geometry{Rows: c.Rows, Columns: c.Columns}, true
		}
	}
	return walk.Geometry{}, false
}

// Has reports whether the site contains a can by this name.
func (s Site) Has(name string) bool {
	_, ok := s.Geometry(name)
	return ok
}

// Validate checks the list is usable and says specifically what is wrong,
// because this is edited by hand and the message is the whole error report.
//
// An empty list is valid. A new install has one, and refusing to save it would
// leave no way to remove the last can.
func (s Site) Validate() error {
	seen := map[string]bool{}
	for i, c := range s.Cans {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return fmt.Errorf("can %d has no name", i+1)
		}
		if seen[strings.ToUpper(name)] {
			return fmt.Errorf("%q appears twice", name)
		}
		seen[strings.ToUpper(name)] = true

		if c.Rows < 1 || c.Columns < 1 {
			return fmt.Errorf("%s: rows and columns must both be at least 1, got %d x %d",
				name, c.Rows, c.Columns)
		}
		if c.Rows*c.Columns > maxPositions {
			return fmt.Errorf("%s: %d x %d is %d positions, which is beyond anything real",
				name, c.Rows, c.Columns, c.Rows*c.Columns)
		}
	}
	return nil
}

// Normalise tidies a list that came from a person: trims names, drops entries
// that are entirely blank, and sorts so a dropdown reads in a sensible order.
func (s Site) Normalise() Site {
	out := Site{}
	for _, c := range s.Cans {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" && c.Rows == 0 && c.Columns == 0 {
			continue // an empty row left behind in the editor
		}
		out.Cans = append(out.Cans, c)
	}
	sortCans(out.Cans)
	return out
}

// sortCans orders by leading letters then by trailing number, so A10 comes
// after A2 rather than before it.
func sortCans(cans []Can) {
	sort.SliceStable(cans, func(i, j int) bool {
		li, ni := splitName(cans[i].Name)
		lj, nj := splitName(cans[j].Name)
		if !strings.EqualFold(li, lj) {
			return strings.ToUpper(li) < strings.ToUpper(lj)
		}
		return ni < nj
	})
}

func splitName(name string) (prefix string, number int) {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(name[i:])
	if err != nil {
		return name, 0
	}
	return name[:i], n
}

// Load reads the site layout. A missing file is not an error: it means a fresh
// install, which starts with no cans.
func Load(path string) (Site, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Empty(), nil
	}
	if err != nil {
		return Site{}, err
	}

	var s Site
	if err := json.Unmarshal(data, &s); err != nil {
		return Site{}, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	if err := s.Validate(); err != nil {
		return Site{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return s, nil
}

// Save writes the layout atomically, so an interrupted write cannot leave a
// file that parses as half a site.
func (s Site) Save(path string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".cans-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

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
