// Command ipreporter is an open-source replacement for Bitmain's IP Reporter.
//
// Phase 0 (what exists today) is capture only: it records the raw UDP that
// miners broadcast when you press their IP-report button, so the parsers for
// v1 can be written against real traffic instead of assumed formats.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"betteripreporter/internal/capture"
	"betteripreporter/internal/parse"
)

// version is set at build time by the release workflow, so it always matches
// the tag it was built from rather than drifting from a hand-edited constant.
var version = "dev"

func main() {
	cmd := "capture"
	args := os.Args[1:]
	// Bare `ipreporter` (or a double-click on Windows) runs a capture, which
	// is the only thing this build does. A leading non-flag word selects a
	// subcommand instead.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "capture":
		os.Exit(runCapture(args))
	case "parse":
		os.Exit(runParse(args))
	case "ports":
		runPorts()
	case "sniff":
		runSniff()
	case "version":
		fmt.Println("ipreporter " + version)
	case "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`ipreporter ` + version + `

  ipreporter                 record miner broadcasts (same as "capture")
  ipreporter capture         record miner broadcasts
  ipreporter parse FILE      decode a capture into miner reports
  ipreporter ports           list the UDP ports capture will listen on
  ipreporter sniff           show the fallback for finding an unknown port
  ipreporter version         print version

capture flags:
  -out DIR        where to write capture files       (default "captures")
  -ports LIST     listen on exactly these ports      e.g. "14235,8888,14200-14300"
  -add LIST       listen on the defaults plus these  e.g. "48899,9527"
  -mute LIST      keep these ports off the screen    e.g. "10001"
                  (they are still recorded in full)
  -quiet          do not print each packet to the screen

`)
}

func runCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ExitOnError)
	out := fs.String("out", "captures", "directory to write capture files into")
	portList := fs.String("ports", "", "listen on exactly these ports instead of the defaults")
	addList := fs.String("add", "", "listen on the default ports plus these")
	muteList := fs.String("mute", "", "keep these ports off the screen (still recorded)")
	quiet := fs.Bool("quiet", false, "do not print each packet to the screen")
	fs.Parse(args)

	ports := capture.DefaultPorts
	if *portList != "" {
		p, err := capture.ParsePorts(*portList)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: -ports: %v\n", err)
			return 2
		}
		ports = p
	}
	if *addList != "" {
		extra, err := capture.ParsePorts(*addList)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: -add: %v\n", err)
			return 2
		}
		ports = capture.Dedup(append(append([]int{}, ports...), extra...))
	}

	var mute []int
	if *muteList != "" {
		m, err := capture.ParsePorts(*muteList)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: -mute: %v\n", err)
			return 2
		}
		mute = m
	}

	fmt.Println()
	fmt.Println("  BetterIPReporter — capture mode (Phase 0)")
	fmt.Println()

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n\n  Stopping...")
		close(stop)
	}()

	err := capture.Run(capture.Options{
		Ports:  ports,
		Mute:   mute,
		OutDir: *out,
		Quiet:  *quiet,
	}, stop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		return 1
	}
	return 0
}

func runParse(args []string) int {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: ipreporter parse captures/capture-....jsonl")
		return 2
	}

	reports, stats, err := parse.ParseFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Printf("\n  %d packets read.  %d were miner reports, collapsing to %d button presses.\n",
		stats.TotalPackets, stats.Decoded, stats.Presses)
	if stats.Collapsed > 0 {
		fmt.Printf("  %d were the miner's own repeat of a report it had just sent.\n", stats.Collapsed)
	}

	if len(reports) > 0 {
		fmt.Printf("\n  %-4s %-12s %-14s %-19s %-9s %s\n", "#", "TIME", "IP", "MAC", "VENDOR", "CAN")
		fmt.Printf("  %s\n", strings.Repeat("─", 68))
		for i, r := range reports {
			can := r.Can
			if can == "" {
				can = "?"
			}
			fmt.Printf("  %-4d %-12s %-14s %-19s %-9s %s\n",
				i+1, r.TS.Format("15:04:05"), r.IP, r.MAC, r.Vendor, can)
		}
	}

	if len(stats.Duplicates) > 0 {
		fmt.Printf("\n  DUPLICATE MACs — these reported more than once, too far apart to be\n")
		fmt.Printf("  the miner repeating itself. Check them before trusting the walk:\n\n")
		for _, d := range stats.Duplicates {
			var times []string
			for _, t := range d.Times {
				times = append(times, t.Format("15:04:05"))
			}
			fmt.Printf("    %s  at %s\n", d.MAC, strings.Join(times, ", "))
		}
	}

	if len(stats.Unparsed) > 0 {
		fmt.Printf("\n  Packets no handler recognised:\n")
		ports := make([]int, 0, len(stats.Unparsed))
		for p := range stats.Unparsed {
			ports = append(ports, p)
		}
		sort.Slice(ports, func(i, j int) bool { return stats.Unparsed[ports[i]] > stats.Unparsed[ports[j]] })
		for _, p := range ports {
			fmt.Printf("    udp/%-6d %5d\n", p, stats.Unparsed[p])
		}
		fmt.Printf("\n  Known formats: %s. If a miner you pressed is missing above,\n", strings.Join(parse.HandlerNames(), ", "))
		fmt.Printf("  send the capture file — its format still needs a handler.\n")
	}
	fmt.Println()
	return 0
}

func runPorts() {
	ports := capture.DefaultPorts
	fmt.Printf("\n  capture listens on these %d UDP ports by default:\n\n", len(capture.Dedup(ports)))
	for i, p := range capture.Dedup(ports) {
		fmt.Printf("%8d", p)
		if (i+1)%8 == 0 {
			fmt.Println()
		}
	}
	fmt.Printf("\n\n  Add more with:  ipreporter capture -add 12345,20000-20100\n\n")
}

// runSniff prints the escape hatch. Binding ports can only hear ports we
// thought to guess; the OS's own packet capture hears everything. This is how
// we pin down a vendor port that is not in the default list.
func runSniff() {
	fmt.Println(`
  FINDING A PORT THE CAPTURE CANNOT SEE

  "ipreporter capture" works by binding UDP ports, so it can only hear the
  ports it was told to listen on. If you press a miner's button and nothing
  appears, the miner is using a port that is not in the list yet.

  To find it, use the packet capture that already ships with your OS. Run one
  of these, press the miner's IP-report button, then send me what it prints.`)

	switch runtime.GOOS {
	case "darwin":
		fmt.Println(`
  On this Mac — find your miner-network interface first:

      ifconfig | grep -B4 "inet "

  Then watch every UDP broadcast on it (replace en0 with your interface).
  It will ask for your password:

      sudo tcpdump -i en0 -n -X -s0 'udp and (dst net 255.255.255.255 or broadcast or multicast)'

  Press the miner's button. The line will look like:

      10.0.1.55.51234 > 255.255.255.255.PORT: UDP, length 56
                                          ^^^^ this is the number I need`)
	case "windows":
		fmt.Println(`
  On Windows 11, pktmon is built in. Open PowerShell as Administrator:

      pktmon filter remove
      pktmon filter add -t UDP
      pktmon start --etw -m real-time

  Press the miner's button, then Ctrl-C and send me the output.`)
	}

	fmt.Print(`
  Once we know the port, it goes into the default list and the plain capture
  picks it up from then on. You can also listen for it immediately:

      ipreporter capture -add PORT

`)
}
