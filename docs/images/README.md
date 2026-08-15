# Screenshots

Drop the images here using exactly these filenames. The README already links to
them, so they appear as soon as they are pushed — nothing else needs editing.

| Filename | What to capture |
|---|---|
| `01-smartscreen.png` | The "Windows protected your PC" dialog, **before** clicking More info |
| `02-run-anyway.png` | Same dialog after **More info**, showing the **Run anyway** button |
| `03-firewall.png` | The Windows Defender Firewall prompt, with **Private networks** ticked |
| `04-app-empty.png` | The app open, before starting — Can dropdown, Rack/Row/Column, Start |
| `05-walking.png` | Mid-walk with a few rows captured |
| `06-cans.png` | The **Cans…** editor open |
| `07-export.png` | The Save dialog from **Export CSV** |

## Before you upload

**Screenshots 05 onward will contain real MAC addresses, IP addresses and can
names.** That is exactly the site data `.gitignore` exists to keep out of this
repo, and a screenshot walks straight past it.

Two safe options:

- **Use the testbench.** Walk a few machines on `34.x` and screenshot that.
- **Blur or block out** the IP and MAC columns before saving. Any image editor
  will do; Paint is enough.

Screenshots 01–04 have no site data in them and are safe as-is.

## Size

**1600px wide** for the app screenshots.

GitHub renders a README about 900px wide, so 1600 gives a sharp image on a
Retina screen without a megabyte per file. Anything wider is just weight.

The trick for uniformity is not resizing afterwards — it is **not resizing the
window between shots**. The app opens at 1180x820, so every capture of it comes
out identically sized on its own. Take all the app screenshots in one sitting,
without dragging the window edges, and they already match.

On a Retina Mac a capture is 2x, so that 1180pt window saves as 2360px. Run the
script to bring them down:

```bash
./docs/images/normalize.sh
```

It scales anything wider than 1600 down, **leaves smaller images alone** (the
Windows dialogs are natively small and enlarging them only makes them blurry),
strips the metadata macOS attaches, and flags any file over 500KB.

The Windows prompts cannot be made to match the app window — Windows decides
their size. That is why the README puts them side by side in a table, where
differing sizes stop mattering.

## Taking them

- **Windows:** <kbd>Win</kbd>+<kbd>Shift</kbd>+<kbd>S</kbd> to snip, or
  <kbd>Alt</kbd>+<kbd>PrtScn</kbd> for just the focused window. Save as PNG.
- **macOS:** <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>4</kbd> then <kbd>Space</kbd>
  to capture a single window.

Crop to the window. Full-desktop shots drag in your taskbar, other windows and
whatever else is open.

## Something to capture with

The built-in <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>5</kbd> plus Preview's markup
tools is enough, and needs nothing installed. If you would rather have a real
tool for it:

- **[Shottr](https://shottr.cc)** — free, and the one worth trying first. Has
  proper blur and redaction, which matters here: it is the easiest way to hide
  the MAC and IP columns before an image goes into a public repo.
- **[Xnapper](https://xnapper.com)** — paid, and aimed squarely at this problem:
  it auto-redacts things that look like addresses, and pads every capture to a
  uniform size. If the redaction step is the part you would forget, this is the
  one that remembers for you.
- **[CleanShot X](https://cleanshot.com)** — paid, the most polished of the
  three. Overkill for seven screenshots.
