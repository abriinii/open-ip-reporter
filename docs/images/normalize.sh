#!/bin/bash
# Normalise screenshots for the README.
#
# Makes every image the same width so they line up down the page, strips the
# metadata macOS attaches to a capture, and reports the sizes so an oversized
# file cannot sneak into the repo unnoticed.
#
# Uses sips, which ships with macOS. Nothing to install.
#
#   ./normalize.sh            # normalise every png here
#   ./normalize.sh 1400       # to a different width

set -euo pipefail
cd "$(dirname "$0")"

WIDTH="${1:-1600}"

shopt -s nullglob
files=(*.png)
if [ ${#files[@]} -eq 0 ]; then
  echo "No .png files here yet. Save your screenshots into this folder first."
  exit 0
fi

printf '%-26s %-16s %-16s %s\n' FILE BEFORE AFTER SIZE
printf '%s\n' "------------------------------------------------------------------------"

for f in "${files[@]}"; do
  before="$(sips -g pixelWidth -g pixelHeight "$f" 2>/dev/null \
            | awk '/pixelWidth/{w=$2} /pixelHeight/{h=$2} END{print w"x"h}')"
  w="${before%x*}"

  # Only ever scale down. Enlarging a small dialog just makes it blurry, and
  # the Windows prompts are natively much smaller than the app window.
  if [ "$w" -gt "$WIDTH" ]; then
    sips --resampleWidth "$WIDTH" "$f" >/dev/null 2>&1
  fi

  # Screenshots carry the capture timestamp and, on some macOS versions, the
  # window title. Neither belongs in a public repo.
  xattr -c "$f" 2>/dev/null || true

  after="$(sips -g pixelWidth -g pixelHeight "$f" 2>/dev/null \
           | awk '/pixelWidth/{w=$2} /pixelHeight/{h=$2} END{print w"x"h}')"
  bytes="$(du -h "$f" | cut -f1 | tr -d ' ')"

  flag=""
  [ "$(du -k "$f" | cut -f1)" -gt 500 ] && flag="  <- over 500KB, consider cropping tighter"
  printf '%-26s %-16s %-16s %s%s\n' "$f" "$before" "$after" "$bytes" "$flag"
done

echo
echo "Done. Add them with:"
echo "    git add docs/images && git commit -m 'Add screenshots' && git push"
