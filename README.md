# BetterIPReporter

An open-source replacement for Bitmain's IP Reporter, for building
**serial → MAC → physical position** maps during a rack walk.

The thing it does that the vendor tool does not: **it records the physical
position with every captured report**, so serial/MAC correlation is a direct
join instead of an assumption that row *n* of a CSV is grid position *n*. One
missed machine no longer silently shifts every pairing after it.

---

## Which file do I download?

Go to the [**Releases**](../../releases/latest) page. There are two different
programs there — you almost certainly want the first one.

| Download this | If you want |
|---|---|
| **`BetterIPReporter-windows-x64.exe`**<br>**`BetterIPReporter-macos-arm64.zip`** | **The app.** A window you walk a rack with. This is the one. |
| `capture-tool-windows-x64.exe`<br>`capture-tool-macos-arm64` | A command-line tool for recording raw miner broadcasts. Only needed when a miner type is not recognised yet and its wire format has to be worked out. |

Nothing to install either way. One file, download and run.

**The laptop must be plugged into the miner network.** The reports are layer-2
UDP broadcasts. They do not cross a router, a VPN, or a Tailscale subnet route
— only a machine physically on that segment can hear them.

### First run on Windows

Two prompts, both expected:

1. **"Windows protected your PC"** (SmartScreen, because the binary is
   unsigned). Click **More info**, then **Run anyway**. If there is no "More
   info" link, right-click the file → **Properties** → tick **Unblock** at the
   bottom → **OK**, then run it again.

2. **Windows Defender Firewall** asks whether to allow it. **Click Allow, and
   make sure "Private networks" is ticked.** Miss this and the app runs but
   hears nothing. An administrator has to click it.

### First run on macOS

Unzip, then right-click the app → **Open** → **Open**. Gatekeeper blocks
unsigned apps on a double-click but allows them through this path. You only do
it once.

For the command-line tool, which is a bare binary rather than an app bundle:

```bash
chmod +x capture-tool-macos-arm64
xattr -d com.apple.quarantine capture-tool-macos-arm64
```

Signing would remove all of this, at $99/year for an Apple Developer account.
Worth it only if this gets handed to people who should not have to be told.

---

## Walking a rack

1. Pick the **Can** and **Rack**, press **Start**.
2. Walk. Press each miner's IP-report button; the row appears on its own. No
   dialog to dismiss.
3. Press **Space** at an empty slot, a switch, or a machine that will not
   report. The position is held open, which is what keeps everything after it
   aligned.
4. Press **Export CSV** at the end of the rack.

The **Row** and **Column** boxes show where the next machine will land. If you
are off by one, type the right numbers in — the walk moves, nothing already
recorded is touched, and the rest of the rack follows on from there.

### Keys

| Key | Does |
|---|---|
| <kbd>Space</kbd> | Skip a position. The only key you press regularly. |
| <kbd>↑</kbd> <kbd>↓</kbd> | Pick a row |
| <kbd>Enter</kbd> | Edit that row's MAC by hand |
| <kbd>N</kbd> | Edit that row's note |
| <kbd>P</kbd> | Set that row's position |
| <kbd>I</kbd> | Insert a blank above it (everything below shifts down) |
| <kbd>Del</kbd> | Delete it (everything below shifts up) |
| <kbd>Ctrl</kbd>+<kbd>Z</kbd> / <kbd>Ctrl</kbd>+<kbd>Y</kbd> | Undo / redo |

Right-clicking a row does the same things, if your hands are already on the
mouse.

### Duplicates are refused, not listed

A MAC already in the rack will not be recorded a second time. You get
`Already recorded at C1/5`, and the position does **not** advance — the next
real machine takes it.

This is deliberate. A duplicate is a double press or a miner heard twice, and
writing it would consume a position belonging to a real machine, shifting
everything after it by one.

Antminers send every report **twice, one second apart**, by design. That pair
is collapsed into one press before any of this applies, so the refusal only
ever fires on a genuine repeat.

### Nothing is lost if the laptop sleeps

The walk is saved to `sessions/` after every capture. Starting the same can and
rack again picks up where it left off.

---

## The exported CSV

One file per rack. Row *n* is grid position *n*, and a skipped position is a
blank row — the same shape the existing process already consumes.

```
21.1.1.43,02:81:f5:ea:e1:db
,
21.1.11.232,02:ad:af:02:ff:45
```

If any row in the rack has a **note**, a third column is added to that file:

```
21.1.1.43,02:81:f5:ea:e1:db,
,,wont ip report
21.1.11.232,02:ad:af:02:ff:45,
```

A rack with no notes exports exactly the two-column form, so nothing changes
downstream for a field usually left blank.

A rack walked halfway exports halfway. It is not padded out to a full rack,
because that would make an unfinished walk look finished.

---

## Cans and racks

| Cans | Rows | Columns | Positions |
|---|---|---|---|
| A1, A2, A5, A6, A7, A8, B1–B4 | 10 | 5 | 50 |
| O1, O2, O3 (outdoor) | 8 | 6 | 48 |

A3 and A4 are out of commission. There is no O4 — `34.x` is the testbench, and
a capture from that range labels itself as such.

The can is derived from the source address of every report: the first octet
encodes it, `1x` → A, `2x` → B, `3x` → O. A machine answering from `15.4.9.113`
is in A5. If a report arrives from a different can than the one being walked,
the app says so rather than recording it quietly.

---

## The capture tool

Only needed when a miner type is not recognised yet.

```
capture-tool                 record miner broadcasts
capture-tool parse FILE      decode a capture into miner reports
capture-tool ports           list the UDP ports it listens on
capture-tool sniff           how to find a port it cannot see
```

It binds ~77 candidate UDP ports and logs every packet with source, port, hex
and ASCII, to a file that can be sent on for a parser to be written against.
Antminer's port 14235 is known; everything else was found this way.

**If a press produces nothing**, the miner is using a port not in the list.
`capture-tool sniff` prints the exact `tcpdump` line for your machine, which
sees every port with nothing to install. One run finds it permanently.

### Background noise is normal

Traffic appears the moment it starts, before you touch a miner. The most common
is a 4-byte `01 00 00 00` on **udp/10001** every ~10 seconds from a pile of
`.254` addresses — that is Ubiquiti UniFi device discovery, not miners.

Repeated identical payloads collapse to a count automatically, and `-mute 10001`
silences a port entirely. Neither affects the capture files: everything
received is always written to disk in full.

---

## Supported miners

| Type | Status |
|---|---|
| **Antminer** | Working. `IP,MAC` as plain ASCII on udp/14235, sent from udp/14236. |
| **Whatsminer** | Not yet. Their own tool can do it, so the miners emit something — it needs a capture to find. |
| **Avalon, SealMiner** | Not yet. Opportunistic. |

Adding a vendor is a new file in `internal/parse`, not a change to anything
that already works.

Note that no vendor's report contains a **serial number** — only IP and MAC.
The serial can only come from the sitemap, which is exactly why position has to
be recorded during the walk. It cannot be recovered afterwards.

---

## Not in this tool, on purpose

- **No network scanning.** BTC Tools already does that well.
- **No config push, firmware, or pool management.**
- **No guessing where a gap is.** If a rack comes up short, it gets reported
  short. Statistical inference from serial prefix and MAC OUI was tested
  against 212 simulated dropouts: 15% uniquely correct, 61% tied, 24% outright
  wrong. A confident wrong answer in a site map is worse than a blank.
- **No sitemap loading or live position validation** in v1. Sitemapping happens
  in Sheets beforehand and that process works; this tool slots into the
  existing workflow without changing anyone else's job.

## Privacy

Captures and exports contain real MAC addresses, IPs and site layout.
`.gitignore` keeps `captures/`, `exports/`, `sessions/`, sitemaps and any
`*.csv` out of the repo. Only files named `sample-*.csv` are allowed in, and
those must be sanitized.

## Building from source

Requires [Go](https://go.dev/dl/) 1.24+. The app additionally needs
[Wails](https://wails.io/docs/gettingstarted/installation) and a C toolchain,
because it embeds the system webview.

```bash
go build -o capture-tool ./cmd/ipreporter    # the command-line tool
cd gui && wails build                        # the app
```

Releases are built automatically. Tagging is the only step needed to publish:

```bash
git tag v1.2.0 && git push origin v1.2.0
```

## License

MIT. See [LICENSE](LICENSE).
