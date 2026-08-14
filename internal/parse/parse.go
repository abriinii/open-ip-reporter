// Package parse turns captured UDP packets into decoded miner reports.
//
// Handlers are registered per vendor and tried in order, so supporting a new
// miner type is a new file in this package rather than a change to anything
// that already works.
package parse

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// doubleFireWindow is how close two identical reports must be to count as one
// press of the button.
//
// Antminers send every report twice, one second apart, by design. Treating the
// second copy as a separate machine would put a phantom in every position;
// treating a genuinely repeated MAC as harmless would let a real duplicate
// through. The window separates the two: inside it is one press, outside it is
// a duplicate worth shouting about.
const doubleFireWindow = 3 * time.Second

// Packet is a captured datagram, as recorded in a .jsonl capture file.
type Packet struct {
	TS      time.Time
	SrcIP   string
	SrcPort int
	DstPort int
	Data    []byte
}

// Report is one decoded miner announcement.
type Report struct {
	Vendor string
	IP     string
	MAC    string
	Serial string // empty when the vendor's format does not carry one
	Can    string // inferred from the source address, may be empty
	TS     time.Time
	Copies int // packets collapsed into this one press
}

// Handler decodes one vendor's wire format. Parse returns false when the
// packet is not this vendor's, which is how the registry picks a handler.
type Handler interface {
	Name() string
	Parse(p Packet) (*Report, bool)
}

// handlers is the registry. Order matters only for packets that more than one
// vendor could plausibly claim, which so far is none.
var handlers = []Handler{
	Antminer{},
}

// Duplicate is a MAC that reported more than once outside the double-fire
// window — a real repeat, not the miner firing twice.
type Duplicate struct {
	MAC   string
	Times []time.Time
}

// Stats describes what a capture file contained.
type Stats struct {
	TotalPackets int
	Decoded      int         // packets a handler understood
	Presses      int         // distinct button presses after collapsing
	Collapsed    int         // packets folded into an earlier press
	Unparsed     map[int]int // count of undecoded packets by destination port
	Duplicates   []Duplicate
}

// ParseFile reads a .jsonl capture and returns the reports it contains, in the
// order they were captured.
func ParseFile(path string) ([]Report, Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Stats{}, err
	}
	defer f.Close()

	stats := Stats{Unparsed: map[int]int{}}
	var reports []Report
	lastSeen := map[string]int{} // MAC -> index in reports
	allSeen := map[string][]time.Time{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec struct {
			TS       string `json:"ts"`
			SrcIP    string `json:"src_ip"`
			SrcPort  int    `json:"src_port"`
			DstPort  int    `json:"dst_port"`
			Hex      string `json:"hex"`
			CanGuess string `json:"can_guess"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, stats, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		data, err := hex.DecodeString(rec.Hex)
		if err != nil {
			return nil, stats, fmt.Errorf("%s line %d: bad hex: %w", path, line, err)
		}
		ts, _ := time.Parse(time.RFC3339Nano, rec.TS)

		stats.TotalPackets++
		pkt := Packet{TS: ts, SrcIP: rec.SrcIP, SrcPort: rec.SrcPort, DstPort: rec.DstPort, Data: data}

		rep, ok := decode(pkt)
		if !ok {
			stats.Unparsed[rec.DstPort]++
			continue
		}
		stats.Decoded++
		rep.Can = rec.CanGuess
		allSeen[rep.MAC] = append(allSeen[rep.MAC], ts)

		// Collapse the vendor's own repeat, but only inside the window.
		if idx, seen := lastSeen[rep.MAC]; seen {
			if ts.Sub(reports[idx].TS) <= doubleFireWindow {
				reports[idx].Copies++
				stats.Collapsed++
				continue
			}
		}
		rep.Copies = 1
		reports = append(reports, *rep)
		lastSeen[rep.MAC] = len(reports) - 1
	}
	if err := sc.Err(); err != nil {
		return nil, stats, err
	}
	stats.Presses = len(reports)

	// Anything that reported as a separate press more than once is a genuine
	// duplicate: a double-tap, or two machines that really do share a MAC.
	counts := map[string]int{}
	for _, r := range reports {
		counts[r.MAC]++
	}
	for mac, n := range counts {
		if n > 1 {
			stats.Duplicates = append(stats.Duplicates, Duplicate{MAC: mac, Times: allSeen[mac]})
		}
	}
	sort.Slice(stats.Duplicates, func(i, j int) bool {
		return stats.Duplicates[i].MAC < stats.Duplicates[j].MAC
	})

	return reports, stats, nil
}

func decode(p Packet) (*Report, bool) {
	for _, h := range handlers {
		if rep, ok := h.Parse(p); ok {
			return rep, true
		}
	}
	return nil, false
}

// HandlerNames lists the vendors currently supported.
func HandlerNames() []string {
	names := make([]string, 0, len(handlers))
	for _, h := range handlers {
		names = append(names, h.Name())
	}
	return names
}
