package capture

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// The site addresses each can on its own /8-style range, with the first octet
// encoding the can name: a leading digit for the letter, a trailing digit for
// the number. 15.1.1.99 is a machine in can A5.
//
// This is a convenience for reading a capture, not a source of truth. It is
// reported as can_guess and always alongside the raw source IP, so a site with
// a different addressing scheme just gets no label rather than a wrong one.
var canLetters = map[int]string{
	1: "A",
	2: "B",
	3: "O",
}

// notACan covers ranges that fit the addressing scheme but are not a can you
// can walk. Labelling them is better than letting them masquerade as one.
var notACan = map[string]string{
	"O4": "testbench", // 34.x is the testbench, not a fourth outdoor can
}

// DeriveCan returns the can name implied by an IP's first octet, or "" when the
// address does not fit the scheme.
func DeriveCan(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	v4 := parsed.To4()
	if v4 == nil {
		return ""
	}
	first := int(v4[0])
	letter, ok := canLetters[first/10]
	if !ok {
		return ""
	}
	number := first % 10
	if number == 0 {
		return "" // x0 is not a can in this scheme
	}
	name := fmt.Sprintf("%s%d", letter, number)
	if label, special := notACan[name]; special {
		return label
	}
	return name
}

// formatCanTable groups the sources seen during a run by the can their address
// implies. This is what turns "16 unexplained subnets" into a readable list of
// which cans were actually heard from.
func formatCanTable(sources map[string]int) string {
	type group struct {
		can     string
		ips     []string
		packets int
	}
	byCan := map[string]*group{}
	unmatched := &group{can: ""}

	for ip, n := range sources {
		can := DeriveCan(ip)
		if can == "" {
			unmatched.ips = append(unmatched.ips, ip)
			unmatched.packets += n
			continue
		}
		g, ok := byCan[can]
		if !ok {
			g = &group{can: can}
			byCan[can] = g
		}
		g.ips = append(g.ips, ip)
		g.packets += n
	}

	if len(byCan) == 0 && len(unmatched.ips) == 0 {
		return ""
	}

	cans := make([]string, 0, len(byCan))
	for c := range byCan {
		cans = append(cans, c)
	}
	// Sort by letter first, then by number, so A2 comes before A10.
	sort.Slice(cans, func(i, j int) bool {
		li, ni := splitCan(cans[i])
		lj, nj := splitCan(cans[j])
		if li != lj {
			return li < lj
		}
		return ni < nj
	})

	var b strings.Builder
	b.WriteString("\n  Sources grouped by can (inferred from the first octet):\n")
	for _, c := range cans {
		g := byCan[c]
		sort.Strings(g.ips)
		fmt.Fprintf(&b, "    %-4s %5d packets   %s\n", g.can, g.packets, summariseIPs(g.ips))
	}
	if len(unmatched.ips) > 0 {
		sort.Strings(unmatched.ips)
		fmt.Fprintf(&b, "    %-4s %5d packets   %s\n", "?", unmatched.packets, summariseIPs(unmatched.ips))
	}
	return b.String()
}

func splitCan(can string) (string, int) {
	if len(can) < 2 {
		return can, 0
	}
	var n int
	fmt.Sscanf(can[1:], "%d", &n)
	return can[:1], n
}

// summariseIPs keeps the summary readable when a can has many machines in it.
func summariseIPs(ips []string) string {
	const max = 4
	if len(ips) <= max {
		return strings.Join(ips, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(ips[:max], ", "), len(ips)-max)
}
