#!/bin/bash
# Import screenshots and give them the names the README expects.
#
# Screenshot tools name files by timestamp, so the images sort into the order
# they were taken — which is the order of the steps. This renames them in that
# order rather than making anyone match nine files to nine names by hand.
#
#   ./import.sh ~/Desktop mac          # show what it would do
#   ./import.sh ~/Desktop mac --apply  # actually do it
#
# Shows its work and changes nothing without --apply, because getting the
# order wrong produces an install guide that confidently shows the wrong step.

set -euo pipefail
cd "$(dirname "$0")"

SRC="${1:-}"
SET="${2:-}"
APPLY="${3:-}"

mac_names=(
  mac-01-download.png
  mac-02-applications.png
  mac-03-open.png
  mac-04-blocked.png
  mac-05-done.png
  mac-06-help.png
  mac-07-open-anyway.png
  mac-08-confirm.png
  mac-09-password.png
)
win_names=(
  win-01-download.png
  win-02-downloaded.png
  win-03-smartscreen.png
  win-04-run-anyway.png
)
app_names=(
  04-app-empty.png
  05-walking.png
  06-cans.png
  07-export.png
)

usage() {
  echo "usage: ./import.sh <folder> <mac|win|app> [--apply]"
  echo
  echo "  mac  9 screenshots, in the order of the install steps"
  echo "  win  4 screenshots"
  echo "  app  4 screenshots: empty app, mid-walk, cans editor, export dialog"
  exit 1
}

[ -z "$SRC" ] && usage
[ -d "$SRC" ] || { echo "No such folder: $SRC"; exit 1; }

case "$SET" in
  mac) names=("${mac_names[@]}") ;;
  win) names=("${win_names[@]}") ;;
  app) names=("${app_names[@]}") ;;
  *)   usage ;;
esac

# Sorted by filename, not by timestamp.
#
# Screenshot tools number sequentially and macOS names by date, so either way
# the names sort into the order the shots were taken. Modification time does
# not: copying or AirDropping a folder rewrites it, and the first file copied
# then looks like the first taken. That happened, and it silently shifted every
# screenshot one step out of place.
files=()
while IFS= read -r f; do files+=("$f"); done < <(
  find "$SRC" -maxdepth 1 -type f \( -iname '*.png' -o -iname '*.jpg' -o -iname '*.jpeg' \) \
    2>/dev/null | sort
)

if [ ${#files[@]} -eq 0 ]; then
  echo "No images found in $SRC"
  exit 1
fi

echo "Found ${#files[@]} image(s) in $SRC, in filename order."
echo "The '$SET' set expects ${#names[@]}."
echo

if [ ${#files[@]} -ne ${#names[@]} ]; then
  echo "!! Counts do not match. Check the folder holds only this set of"
  echo "   screenshots, or rename the odd ones out by hand."
  echo
fi

n=${#names[@]}
[ ${#files[@]} -lt "$n" ] && n=${#files[@]}

for ((i = 0; i < n; i++)); do
  printf '  %-40s ->  %s\n' "$(basename "${files[$i]}")" "${names[$i]}"
done

echo
if [ "$APPLY" != "--apply" ]; then
  echo "Nothing copied. Check the order above, then run again with --apply:"
  echo "    ./import.sh \"$SRC\" $SET --apply"
  exit 0
fi

for ((i = 0; i < n; i++)); do
  cp "${files[$i]}" "${names[$i]}"
done
echo "Copied $n file(s)."
echo
./normalize.sh
