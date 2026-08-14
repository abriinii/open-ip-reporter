# OpenIPReporter

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
| **`OpenIPReporter-windows-x64.exe`**<br>**`OpenIPReporter-macos-arm64.zip`** | **The app.** A window you walk a rack with. This is the one. |
| `capture-tool-windows-x64.exe`<br>`capture-tool-macos-arm64` | A command-line tool for recording raw miner broadcasts. Only needed when a miner type is not recognised yet and its wire format has to be worked out. |

Nothing to install either way. One file, download and run.

**The laptop must be plugged into the switch in the can you are walking.** The
reports are layer-2 UDP broadcasts. They do not cross a router, a VPN, or a
Tailscale subnet route — only a machine on that segment can hear them.

This bites hardest with Whatsminers. Antminer reports may still reach a desk
elsewhere on site depending on how the network is bridged, which makes it look
as though the tool works from anywhere; Whatsminers will simply never arrive.
**If one miner type reports and another does not, check where you are plugged
in before suspecting the software.**

### First run on Windows

Two prompts, both expected:

1. **"Windows protected your PC"** (SmartScreen). Click **More info**, then
   **Run anyway**. See [Why the warning appears](#why-the-warning-appears) —
   it is not a virus detection, and there is a way to stop seeing it.

2. **Windows Defender Firewall** asks whether to allow it. **Click Allow, and
   make sure "Private networks" is ticked.** Miss this and the app runs but
   hears nothing. An administrator has to click it.

### Why the warning appears

SmartScreen is not saying the file is malicious. It is saying it has no
*reputation* — it has not been downloaded enough times for Microsoft to have an
opinion about it. A tool used by one site will never accumulate that.

Worth knowing before spending money on it: **code signing no longer removes
this.** EV certificates used to bypass SmartScreen on first download, and
[Microsoft removed that behaviour in 2024](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options).
Signed files now build reputation exactly like unsigned ones. Paying for a
certificate would change the publisher shown in the dialog, not remove it.

What actually works, cheapest first:

- **Copy it from an internal share or a USB stick instead of downloading it in
  a browser.** The prompt is triggered by the Mark of the Web, a tag browsers
  attach to downloaded files. Files copied from a file share or removable media
  usually do not carry it, and no prompt appears. For an internally distributed
  tool this is the whole fix.
- **Unblock an already-downloaded copy:** right-click → **Properties** → tick
  **Unblock** → **OK**. Same effect, one file at a time.
- **Deploy a certificate to your own machines.** If the fleet is managed with
  Group Policy or Intune, IT can trust a self-signed certificate across it and
  the warning stops entirely on those machines. Only worth it for wider rollout.
- **[SignPath Foundation](https://signpath.org)** offers free code signing to
  qualifying open-source projects, which this is. It does not grant instant
  reputation either, but it costs nothing and names a real publisher. See
  [CODE_SIGNING_POLICY.md](CODE_SIGNING_POLICY.md), which their terms require
  a project to publish before applying.
- **[Azure Artifact Signing](https://azure.microsoft.com/en-us/products/artifact-signing)**
  at about $9.99/month is the paid option Microsoft now recommends. An OV
  certificate from a CA runs $150–300/year for the same SmartScreen behaviour.

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
4. Press **Stop** at the end of the rack, then **Export CSV**. Stopping keeps
   the rack on screen — it ends the walk, it does not discard it.

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
| <kbd>Ctrl</kbd>+<kbd>E</kbd> | Export (once stopped) |

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

## Using it at another site

The can list is not baked into the program. Press **Cans…** next to the can
dropdown to add, edit or remove cans and set the rack shape for each one.

It is saved as `cans.json` next to the app, so setting up a second machine is a
matter of copying that one file across. A fresh install writes the list below
as a starting point; replacing it entirely is the expected thing to do
somewhere else.

One thing that does not travel: the can is normally inferred from a report's
source address using this site's addressing scheme. Somewhere addressed
differently that inference simply does not apply, and the app stays quiet
rather than warning about cans that do not exist there.

## Cans and racks

The defaults for this site, editable under **Cans…**:

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

| Type | Port | Payload | Status |
|---|---|---|---|
| **Antminer** | udp/14235 | `10.0.0.5,aa:bb:cc:dd:ee:ff` | Working |
| **Whatsminer** | udp/8888 | `IP:10.0.0.5MAC:aa:bb:cc:dd:ee:ff` | Working |
| Avalon, SealMiner | — | — | Not yet |

> **Whatsminer's button must be held for more than five seconds.** A short
> press does nothing at all — no broadcast is sent. Hold it until the two
> right-hand LEDs flash. This is the single most likely reason a Whatsminer
> appears not to report.

Both vendors are heard by the same listener at the same time, so a mixed can
is one walk rather than two tools.

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

## Update notice

On startup the app asks GitHub whether a newer release exists, and shows the
release notes if so. It never downloads or installs anything — the button opens
the download page in your browser.

Turn it off with the checkbox on that notice. Off, the app makes no outbound
network requests at all. It also stays quiet when there is no route to the
internet, which is the usual case standing in a can.

Nothing about your site is sent. The request carries only the program name and
its version. See [CODE_SIGNING_POLICY.md](CODE_SIGNING_POLICY.md#privacy).

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
go build -o capture-tool ./cmd/capture-tool    # the command-line tool
cd gui && wails build                        # the app
```

Releases are built automatically. Tagging is the only step needed to publish:

```bash
git tag v1.2.0 && git push origin v1.2.0
```

## License

MIT. See [LICENSE](LICENSE).
