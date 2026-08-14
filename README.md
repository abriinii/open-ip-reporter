# BetterIPReporter

An open-source replacement for Bitmain's IP Reporter, for building
**serial → MAC → physical position** maps during a rack walk.

The thing it does that the vendor tool does not: **it records the physical
position with every captured report**, so serial/MAC correlation is a direct
join instead of an assumption that row *n* of a CSV is grid position *n*. One
missed machine no longer silently shifts every pairing after it.

> **Status: Phase 0 — capture only.**
> This build records what miners broadcast so the parsers can be written
> against real traffic. It does not yet track position or export CSVs.
> See [What works today](#what-works-today).

---

## Download and run

1. Go to the [**Releases**](../../releases/latest) page and download the file for your machine:
   - **macOS (Apple Silicon)** — `ipreporter-macos-arm64`
   - **Windows 11** — `ipreporter-windows-x64.exe`

2. **The laptop must be plugged into the miner network.** The reports are
   layer-2 UDP broadcasts. They do not cross a router, a VPN, or a Tailscale
   subnet route — only a machine physically on that segment can hear them.

### Windows

Double-click `ipreporter-windows-x64.exe`. Two prompts, both expected:

1. **"Windows protected your PC"** (SmartScreen, because the binary is
   unsigned). Click **More info**, then **Run anyway**. If there is no "More
   info" link, right-click the file → **Properties** → tick **Unblock** at the
   bottom → **OK**, then run it again.

2. **Windows Defender Firewall** asks whether to allow it. **Click Allow, and
   make sure "Private networks" is ticked.** If you miss this prompt the tool
   runs but hears nothing. An administrator has to click it.

### macOS

macOS blocks unsigned downloads, so it takes two commands the first time. In
Terminal, in the folder you downloaded to:

```bash
chmod +x ipreporter-macos-arm64 && xattr -d com.apple.quarantine ipreporter-macos-arm64
```

Then run it:

```bash
./ipreporter-macos-arm64
```

If macOS still refuses, open **System Settings → Privacy & Security**, scroll
down, and click **Open Anyway** next to the blocked app.

<sub>Signing the binaries would remove this step entirely. It costs $99/year for
an Apple Developer account plus a notarization step in the release workflow.
Worth doing if handing this to other techs becomes routine; not worth it for
one laptop.</sub>

---

## What works today

Press a miner's IP-report button and the tool records the broadcast:

```
#1  14:22:07.418
  from 10.4.19.55:51234  ->  udp/14235   (56 bytes)
  ascii: ...........Z;.10.4.19.55.
  00000000  00 01 00 14 00 00 00 00  c4 11 1e 5a 3b 7f 31 30  |...........Z;.10|
  00000010  2e 34 2e 31 39 2e 35 35  00                       |.4.19.55.|
```

Press **Ctrl-C** to stop. It writes two files into a `captures/` folder:

| File | What it is |
|---|---|
| `capture-*.jsonl` | One JSON record per packet. This is the one parsers get built from. |
| `capture-*.txt` | The same data as readable hex dumps, plus your network interfaces and a summary. |

**Send both files back.** That is the whole job of Phase 0.

### What to capture

One button press per miner type is enough, but a few of each is better:

- **Antminer** — the known case, confirms the tool works.
- **Whatsminer** (`BTM`/`ZDM`/`HTM` serials) — the important unknown. Their own
  WhatsMinerTool can do IP reports, so the miners emit *something*; we need to
  see what and on which port.
- **Avalon** (`AME`) and **SealMiner** (`S12`) — grab them if convenient.

Note which miner type you pressed and roughly when, so the packets can be told
apart in the log.

### Commands

```
ipreporter                 record miner broadcasts
ipreporter ports           list the UDP ports it listens on
ipreporter sniff           how to find a port it cannot see (read this if a press produces nothing)
ipreporter version         print version
```

Flags for `capture`:

```
-out DIR        where to write capture files       (default "captures")
-ports LIST     listen on exactly these ports      e.g. "14235,8888,14200-14300"
-add LIST       listen on the defaults plus these  e.g. "48899,9527"
-mute LIST      keep these ports off the screen    e.g. "10001"
                (they are still recorded in full)
-quiet          do not print each packet to the screen
```

### If you press a button and nothing appears

The tool hears a port by **binding** it, so it only hears ports it was told
about. 14235 (Antminer) is known; other vendors' ports are guesses right now.

Run `ipreporter sniff`. It prints the exact `tcpdump` command for your Mac —
tcpdump ships with macOS, needs no install, and sees **every** port. One run
identifies the port permanently.

### Background noise is normal

The moment you start it you will see traffic, before you touch a single miner.
That is the network talking to itself, and it means the tool is working.

The most common one on a mining site is a 4-byte `01 00 00 00` on **udp/10001**
arriving every ~10 seconds from a pile of `.254` addresses. That is **Ubiquiti
UniFi device discovery** — your switches and gateways announcing themselves,
not miners.

Two things keep it from burying a real report:

- **Repeated identical payloads collapse automatically.** The first few print in
  full, then the tool counts the rest instead of printing them. Two miners
  reporting always produce two *different* payloads (the MAC is in there), so
  this only ever hides beacons.
- **`-mute` silences a port entirely** if you want it gone:

  ```
  ipreporter capture -mute 10001
  ```

Neither one affects the capture files. **Everything received is always written
to disk in full**, no matter what the screen shows. The end-of-run summary
labels each port so infrastructure chatter is easy to tell apart from a port
worth investigating.

### Other notes on reading a capture
- **A few ports failing to bind is normal.** "address already in use" means
  another program holds it; "permission denied" means it is below 1024 and
  needs admin. Everything else still binds, and the failures are listed at the
  end of the `.txt` file. Only worry if 14235 fails — the tool warns loudly if
  it does.
- The capture can run **at the same time as Bitmain's IP Reporter** — it shares
  port 14235 rather than fighting for it. Running both and pressing a button is
  the quickest way to confirm it is really working.

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

Captures contain real MAC addresses, IPs and site layout. `.gitignore` is set
up to keep `captures/`, `exports/`, sitemaps and any `*.csv` out of the repo.
Only files named `sample-*.csv` are allowed in, and those must be sanitized.

## Building from source

Requires [Go](https://go.dev/dl/) 1.24+.

```bash
go build -o ipreporter ./cmd/ipreporter
```

Releases are built automatically. Tagging is the only step needed to publish:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

## License

MIT. See [LICENSE](LICENSE).
