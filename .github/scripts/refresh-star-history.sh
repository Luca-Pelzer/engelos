#!/usr/bin/env bash
#
# Refresh the self-hosted star-history chart.
#
# Why this exists: star-history.com embeds the repo owner's GitHub avatar as an
# *external* <image href="https://avatars.githubusercontent.com/..."> inside the
# SVG. GitHub's image proxy (Camo) refuses to load external images nested inside
# an SVG, so the avatar renders as a broken-image glyph when the chart is hot-
# linked in the README. This script downloads the chart, inlines the avatar as a
# base64 data URI (so there is no external reference left to block), and writes a
# self-hosted copy to .github/assets/star-history.svg that renders cleanly.
#
# Trade-off: the self-hosted SVG is a snapshot. Re-run this script (or wire it
# into a scheduled workflow) to update it as the star count grows.
#
# Usage:  bash .github/scripts/refresh-star-history.sh
set -euo pipefail

REPO="Luca-Pelzer/engelos"
OWNER_UID="124480067"   # github user id for the avatar embedded by star-history
OUT=".github/assets/star-history.svg"
UA="Mozilla/5.0 (compatible; engelos-readme-bot)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Fetching star-history chart for $REPO ..."
curl -sL -A "$UA" --max-time 60 \
  "https://api.star-history.com/svg?repos=${REPO}&type=Date" -o "$tmp/chart.svg"

echo "Fetching owner avatar (uid $OWNER_UID) ..."
curl -sL -A "$UA" --max-time 30 \
  "https://avatars.githubusercontent.com/u/${OWNER_UID}" -o "$tmp/avatar.png"

# Inline the avatar: replace the external href with a base64 data URI.
python3 - "$tmp/chart.svg" "$tmp/avatar.png" "$OUT" "$OWNER_UID" <<'PY'
import base64, re, sys
chart_path, avatar_path, out_path, uid = sys.argv[1:5]
svg = open(chart_path, encoding="utf-8").read()
b64 = base64.b64encode(open(avatar_path, "rb").read()).decode("ascii")
data_uri = f"data:image/png;base64,{b64}"
# Swap any external avatar reference (href or xlink:href) for the data URI.
pattern = re.compile(r'(xlink:href|href)="https://avatars\.githubusercontent\.com/u/' + re.escape(uid) + r'[^"]*"')
svg, n = pattern.subn(lambda m: f'{m.group(1)}="{data_uri}"', svg)
open(out_path, "w", encoding="utf-8").write(svg)
print(f"Inlined avatar into {n} reference(s) -> {out_path}")
if n == 0:
    print("WARNING: no external avatar reference found; chart format may have changed.", file=sys.stderr)
PY

echo "Done. Self-hosted chart written to $OUT"
