// Package walk models the physical walk: where you are in a rack, and what was
// recorded at each position.
//
// Everything here is deliberately independent of any user interface, because
// this is where correctness lives. A wrong row is worse than a slow walk.
package walk

import "fmt"

// Geometry describes the shape of one rack.
type Geometry struct {
	Rows    int // positions top to bottom in a column
	Columns int
}

// Positions is the number of slots in a rack of this shape.
func (g Geometry) Positions() int { return g.Rows * g.Columns }

// Valid reports whether the geometry could describe a real rack here. The
// upper bound is as much a part of that as the lower: a rack holding more than
// MaxPositions does not exist on this site, so accepting one would only let a
// typo produce positions no machine can occupy.
func (g Geometry) Valid() bool {
	return g.Rows > 0 && g.Columns > 0 && g.Positions() <= MaxPositions
}

// geometryByCan holds the shapes observed on site, as starting points rather
// than truth. A rack that has been rebuilt, or refilled with a smaller frame,
// is a data-entry problem and not a reason to recompile — the session carries
// its own geometry and the operator can change it before starting a walk.
//
// Fifty is the most machines any single rack here holds. Outdoor cans are
// shorter at eight rows, but fit six across when filled with Whatsminers,
// whose frame is narrower.
//
// A3 and A4 are permanently out of commission and deliberately absent, though
// their switches are still on the network. There is no O4: 34.x is the
// testbench.
var geometryByCan = map[string]Geometry{
	"A1": {Rows: 10, Columns: 5}, // 50
	"A2": {Rows: 10, Columns: 5},
	"A5": {Rows: 10, Columns: 5},
	"A6": {Rows: 10, Columns: 5},
	"A7": {Rows: 10, Columns: 5},
	"A8": {Rows: 10, Columns: 5},
	"B1": {Rows: 10, Columns: 5},
	"B2": {Rows: 10, Columns: 5},
	"B3": {Rows: 10, Columns: 5},
	"B4": {Rows: 10, Columns: 5},
	"O1": {Rows: 8, Columns: 6}, // 48: shorter, but six across
	"O2": {Rows: 8, Columns: 6},
	"O3": {Rows: 8, Columns: 6},
}

// MaxPositions is the most machines any rack on this site holds. Geometry
// entered by hand is checked against it, so a typo cannot create a rack that
// could not physically exist.
const MaxPositions = 50

// DefaultGeometry returns the known shape for a can. The second result is false
// for a can we have no record of, so a caller can ask rather than assume.
func DefaultGeometry(can string) (Geometry, bool) {
	g, ok := geometryByCan[can]
	return g, ok
}

// Position is a physical slot: which can, which rack in it, and the column and
// row within that rack. Columns and rows are 1-based, matching how they are
// counted on the floor rather than how a computer would index them.
type Position struct {
	Can    string `json:"can"`
	Rack   int    `json:"rack"`
	Column int    `json:"column"`
	Row    int    `json:"row"`
}

func (p Position) String() string {
	return fmt.Sprintf("%s R%d C%d/%d", p.Can, p.Rack, p.Column, p.Row)
}

// Short is the column/row alone, for when the can and rack are already on
// screen and repeating them is noise.
func (p Position) Short() string {
	return fmt.Sprintf("C%d/%d", p.Column, p.Row)
}

// Valid reports whether a position falls inside a rack of the given shape.
func (p Position) Valid(g Geometry) bool {
	return p.Can != "" && p.Rack > 0 &&
		p.Column >= 1 && p.Column <= g.Columns &&
		p.Row >= 1 && p.Row <= g.Rows
}

// Index is how many steps into the walk this position sits, counting from zero
// at column 1 row 1. Inverse of PositionAt.
func (p Position) Index(g Geometry) int {
	return (p.Column-1)*g.Rows + (p.Row - 1)
}

// Next returns the following position in walk order: down a column top to
// bottom, then across to the top of the next column. The second result is false
// at the end of the last column, where the rack is finished.
func (p Position) Next(g Geometry) (Position, bool) {
	if p.Row < g.Rows {
		p.Row++
		return p, true
	}
	if p.Column < g.Columns {
		p.Column++
		p.Row = 1
		return p, true
	}
	return p, false
}

// Advance moves n steps along the walk order. It reports false if that would
// run off the end of the rack.
func (p Position) Advance(g Geometry, n int) (Position, bool) {
	for i := 0; i < n; i++ {
		next, ok := p.Next(g)
		if !ok {
			return p, false
		}
		p = next
	}
	return p, true
}

// PositionAt returns the position n steps into a rack, counting from zero.
// Inverse of Position.Index.
func PositionAt(can string, rack int, g Geometry, index int) (Position, bool) {
	if index < 0 || index >= g.Positions() {
		return Position{}, false
	}
	return Position{
		Can:    can,
		Rack:   rack,
		Column: index/g.Rows + 1,
		Row:    index%g.Rows + 1,
	}, true
}
