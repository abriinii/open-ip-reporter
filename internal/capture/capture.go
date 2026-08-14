// Package capture is a raw UDP listener that records every packet it can hear,
// with no assumptions about payload format.
//
// The point is to learn the wire formats from real miners rather than guessing
// them. Nothing in here parses a payload; that lives in the parse package, so
// a new miner type is a new file there rather than a change in here.
package capture

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// repeatShowLimit is how many copies of an identical payload get printed in
// full before the console collapses them to a count. The capture files are
// never affected by this.
const repeatShowLimit = 3

// Options controls a capture run.
type Options struct {
	Ports  []int  // UDP ports to bind
	Mute   []int  // ports to keep off the console (still fully recorded)
	OutDir string // directory to write capture files into
	Quiet  bool   // suppress per-packet console output
}

// Packet is one received datagram.
type Packet struct {
	TS      time.Time
	SrcIP   string
	SrcPort int
	DstPort int
	Data    []byte
}

// jsonRecord is the machine-readable form written to the .jsonl file, one per
// line. This is the file that matters for building parsers later.
type jsonRecord struct {
	TS       string `json:"ts"`
	SrcIP    string `json:"src_ip"`
	SrcPort  int    `json:"src_port"`
	DstPort  int    `json:"dst_port"`
	Len      int    `json:"len"`
	Hex      string `json:"hex"`
	ASCII    string `json:"ascii"`
	CanGuess string `json:"can_guess,omitempty"` // inferred, never authoritative
}

// Run binds every port in opts.Ports and records traffic until the returned
// stop function is called or the process is interrupted.
func Run(opts Options, stop <-chan struct{}) error {
	raiseFDLimit()

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	stamp := time.Now().Format("20060102-150405")
	jsonlPath := filepath.Join(opts.OutDir, "capture-"+stamp+".jsonl")
	textPath := filepath.Join(opts.OutDir, "capture-"+stamp+".txt")

	jsonlFile, err := os.Create(jsonlPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", jsonlPath, err)
	}
	defer jsonlFile.Close()

	textFile, err := os.Create(textPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", textPath, err)
	}
	defer textFile.Close()

	writeEnvironment(textFile, opts.Ports)

	// Bind every port. Failures are expected and non-fatal: some ports need
	// privileges (anything under 1024), and some may be held by another
	// process that does not permit sharing. We report the tally and carry on
	// with whatever bound.
	conns, bindErrs := bindAll(opts.Ports)
	if len(conns) == 0 {
		return fmt.Errorf("could not bind any of the %d requested ports", len(opts.Ports))
	}

	fmt.Printf("  Listening on %d of %d ports.", len(conns), len(opts.Ports))
	if len(bindErrs) > 0 {
		fmt.Printf(" %d unavailable (normal — see %s).", len(bindErrs), filepath.Base(textPath))
	}
	fmt.Println()
	if bound := portOf(conns, 14235); bound {
		fmt.Println("  Port 14235 (Antminer) is bound and ready.")
	} else {
		fmt.Println("  WARNING: port 14235 (Antminer) could NOT be bound.")
	}
	fmt.Printf("\n  Writing to %s\n", jsonlPath)
	fmt.Println("\n  Go press the IP-report button on a miner. Press Ctrl-C when done.")
	fmt.Println(strings.Repeat("─", 72))

	packets := make(chan Packet, 256)
	var readers sync.WaitGroup
	for _, c := range conns {
		readers.Add(1)
		go func(c *net.UDPConn) {
			defer readers.Done()
			readLoop(c, packets)
		}(c)
	}

	muted := make(map[int]bool, len(opts.Mute))
	for _, p := range opts.Mute {
		muted[p] = true
	}

	// One writer goroutine owns both files, so no locking is needed and the
	// two files stay in the same order.
	counts := map[int]int{}
	sources := map[string]int{}
	repeats := map[string]int{}
	total, hidden := 0, 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		enc := json.NewEncoder(jsonlFile)
		for p := range packets {
			total++
			counts[p.DstPort]++
			sources[p.SrcIP]++
			hexStr := hex.EncodeToString(p.Data)

			enc.Encode(jsonRecord{
				TS:       p.TS.Format(time.RFC3339Nano),
				SrcIP:    p.SrcIP,
				SrcPort:  p.SrcPort,
				DstPort:  p.DstPort,
				Len:      len(p.Data),
				Hex:      hexStr,
				ASCII:    printableASCII(p.Data),
				CanGuess: DeriveCan(p.SrcIP),
			})
			jsonlFile.Sync() // flush every packet: a crash must not lose the walk

			block := formatPacket(total, p)
			io.WriteString(textFile, block)
			textFile.Sync()

			// Everything above is unconditional: the files always get every
			// packet. Only the console is filtered, so that infrastructure
			// chatter cannot bury the one packet you walked over to see.
			if opts.Quiet {
				continue
			}

			// Identical payload on the same port, over and over, is a beacon
			// rather than a miner: two miners reporting produce two different
			// payloads, because the MAC is in there. Show the first few, then
			// count the rest.
			key := fmt.Sprintf("%d|%s", p.DstPort, hexStr)
			repeats[key]++

			switch {
			case muted[p.DstPort]:
				hidden++
			case repeats[key] < repeatShowLimit:
				io.WriteString(os.Stdout, block)
			case repeats[key] == repeatShowLimit:
				io.WriteString(os.Stdout, block)
				fmt.Printf("  ^ udp/%d keeps repeating this exact payload — background\n"+
					"    chatter, not a miner. Further copies will be counted instead of\n"+
					"    printed. Every one is still recorded in the capture files.\n"+
					"    Hide this port entirely with:  -mute %d\n",
					p.DstPort, p.DstPort)
			default:
				hidden++
			}

			// A heartbeat, so a screen full of nothing still looks alive.
			if hidden > 0 && hidden%200 == 0 {
				fmt.Printf("  … still listening (%d background packets hidden so far)\n", hidden)
			}
		}
	}()

	<-stop

	for _, c := range conns {
		c.Close()
	}
	readers.Wait()
	close(packets)
	<-done

	summary := formatSummary(total, hidden, counts, sources, bindErrs)
	io.WriteString(textFile, summary)
	fmt.Print(summary)

	fmt.Printf("\n  Capture files written:\n    %s\n    %s\n\n", jsonlPath, textPath)
	if total == 0 {
		fmt.Println("  No packets captured. If you pressed a miner button and saw nothing,")
		fmt.Println("  run `capture-tool sniff` for the fallback that can see every port.")
	} else {
		fmt.Println("  Send both files back and I'll build the parser from them.")
	}
	return nil
}

// Listen binds ports and hands every datagram to onPacket until stop is
// closed. It is the same receive path as Run without the file writing, for
// callers that want to react to reports live rather than review them later.
//
// onPacket is called from a single goroutine, so it does not need to be safe
// for concurrent use, but it must not block for long: everything else is
// waiting behind it.
func Listen(ports []int, onPacket func(Packet), stop <-chan struct{}) (bound int, err error) {
	raiseFDLimit()

	conns, _ := bindAll(ports)
	if len(conns) == 0 {
		return 0, fmt.Errorf("could not bind any of the %d requested ports", len(ports))
	}

	packets := make(chan Packet, 256)
	var readers sync.WaitGroup
	for _, c := range conns {
		readers.Add(1)
		go func(c *net.UDPConn) {
			defer readers.Done()
			readLoop(c, packets)
		}(c)
	}

	go func() {
		<-stop
		for _, c := range conns {
			c.Close()
		}
		readers.Wait()
		close(packets)
	}()

	go func() {
		for p := range packets {
			onPacket(p)
		}
	}()

	return len(conns), nil
}

// bindAll opens one UDP socket per port on 0.0.0.0, which is what receives
// both subnet broadcasts and 255.255.255.255. Returns the sockets that opened
// and a description of each that did not.
func bindAll(ports []int) ([]*net.UDPConn, []string) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(setReuse)
		},
	}

	var conns []*net.UDPConn
	var errs []string
	for _, p := range ports {
		pc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", p))
		if err != nil {
			errs = append(errs, fmt.Sprintf("  port %-6d %v", p, err))
			continue
		}
		uc, ok := pc.(*net.UDPConn)
		if !ok {
			pc.Close()
			errs = append(errs, fmt.Sprintf("  port %-6d unexpected socket type", p))
			continue
		}
		conns = append(conns, uc)
	}
	return conns, errs
}

func readLoop(c *net.UDPConn, out chan<- Packet) {
	dstPort := c.LocalAddr().(*net.UDPAddr).Port
	buf := make([]byte, 65536)
	for {
		n, addr, err := c.ReadFromUDP(buf)
		if err != nil {
			return // socket closed on shutdown
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		out <- Packet{
			TS:      time.Now(),
			SrcIP:   addr.IP.String(),
			SrcPort: addr.Port,
			DstPort: dstPort,
			Data:    data,
		}
	}
}

func portOf(conns []*net.UDPConn, want int) bool {
	for _, c := range conns {
		if c.LocalAddr().(*net.UDPAddr).Port == want {
			return true
		}
	}
	return false
}

func formatPacket(n int, p Packet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n#%d  %s\n", n, p.TS.Format("15:04:05.000"))
	can := ""
	if c := DeriveCan(p.SrcIP); c != "" {
		can = "   can " + c + "?"
	}
	fmt.Fprintf(&b, "  from %s:%d  ->  udp/%d   (%d bytes)%s\n",
		p.SrcIP, p.SrcPort, p.DstPort, len(p.Data), can)
	fmt.Fprintf(&b, "  ascii: %s\n", printableASCII(p.Data))
	for _, line := range strings.Split(strings.TrimRight(hex.Dump(p.Data), "\n"), "\n") {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}

// knownPorts labels traffic we have already identified, so a port that is just
// site infrastructure is not mistaken for a miner that needs investigating.
var knownPorts = map[int]string{
	14235: "Antminer IP Reporter  <- what we want",
	10001: "Ubiquiti UniFi device discovery (background)",
	5353:  "mDNS / Bonjour (background)",
	1900:  "SSDP (background)",
	137:   "NetBIOS name service (background)",
	138:   "NetBIOS datagram (background)",
	67:    "DHCP server (background)",
	68:    "DHCP client (background)",
	5355:  "LLMNR (background)",
	123:   "NTP (background)",
}

func formatSummary(total, hidden int, counts map[int]int, sources map[string]int, bindErrs []string) string {
	var b strings.Builder
	b.WriteString("\n" + strings.Repeat("─", 72) + "\n")
	fmt.Fprintf(&b, "\n  SUMMARY\n\n  %d packets from %d distinct source addresses.\n",
		total, len(sources))
	if hidden > 0 {
		fmt.Fprintf(&b, "  %d were hidden from the screen as repetitive background traffic.\n"+
			"  All of them are in the capture files regardless.\n", hidden)
	}

	if len(counts) > 0 {
		ports := make([]int, 0, len(counts))
		for p := range counts {
			ports = append(ports, p)
		}
		sort.Slice(ports, func(i, j int) bool { return counts[ports[i]] > counts[ports[j]] })
		b.WriteString("\n  Packets by destination port:\n")
		for _, p := range ports {
			label := knownPorts[p]
			if label == "" {
				label = "unidentified — worth a look"
			}
			fmt.Fprintf(&b, "    udp/%-6d %5d   %s\n", p, counts[p], label)
		}
	}

	b.WriteString(formatCanTable(sources))

	if len(bindErrs) > 0 {
		b.WriteString("\n  Ports that could not be bound (informational):\n")
		for _, e := range bindErrs {
			b.WriteString(e + "\n")
		}
	}
	return b.String()
}

// writeEnvironment records the host's network interfaces at the top of the
// text log. If a capture comes back empty, this is usually what explains it —
// wrong interface, wrong subnet, or VPN routing the traffic away.
func writeEnvironment(w io.Writer, ports []int) {
	fmt.Fprintf(w, "OpenIPReporter capture\n")
	fmt.Fprintf(w, "started: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "host os: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(w, "ports requested: %d\n\n", len(ports))

	fmt.Fprintf(w, "network interfaces:\n")
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Fprintf(w, "  (could not enumerate: %v)\n", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		var ips []string
		for _, a := range addrs {
			ips = append(ips, a.String())
		}
		if len(ips) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-12s %-18s %s\n", ifc.Name, ifc.HardwareAddr, strings.Join(ips, " "))
	}
	fmt.Fprintf(w, "\n%s\n", strings.Repeat("─", 72))
}

// printableASCII renders the payload with non-printable bytes as dots, which is
// usually enough to tell at a glance whether a format is text or binary.
func printableASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 0x20 && c < 0x7f {
			out[i] = c
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
