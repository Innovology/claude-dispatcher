#!/usr/bin/env bash
# Regenerate the README screenshots.
#
# Runs the real binary against a fictional fixture (see fixture.py) inside tmux
# at the design's own 176x40, captures each lens with its colours, and converts
# the capture to SVG. Nothing here ships in the binary — the cockpit has no demo
# mode, so a screenshot is just a real render of invented records.
#
#   ./docs/screenshots/generate.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
work="${TMPDIR:-/tmp}/cd-shots"
sess="cdshots$$"

cleanup() { tmux kill-session -t "$sess" 2>/dev/null || true; }
trap cleanup EXIT

command -v tmux >/dev/null || { echo "tmux is required" >&2; exit 1; }

echo "building…"
go build -o "$work.bin" "$repo"
python3 "$here/fixture.py" "$work"

tmux new-session -d -s "$sess" -x 176 -y 40 \
  "HOME=$work/home CLAUDE_DISPATCHER_STATE=$work/state $work.bin"

# The first snapshot reads git and the forges; wait for it rather than
# screenshotting the loading state.
for _ in $(seq 1 60); do
  out="$(tmux capture-pane -p -t "$sess" 2>/dev/null || true)"
  [ -n "$out" ] && ! grep -q "reading your dispatch" <<<"$out" && break
  sleep 2
done

shoot() { # name  title  key...
  local name="$1" title="$2"; shift 2
  for k in "$@"; do tmux send-keys -t "$sess" -l "$k"; sleep 1; done
  sleep 1
  tmux capture-pane -pe -t "$sess" | python3 "$here/ansi2svg.py" "$title" > "$here/../$name.svg"
  echo "  docs/$name.svg"
}

echo "capturing…"
shoot triage    "Triage: the fleet, ranked"        1
shoot working   "The running filter"               w
shoot products  "The products lens"                w 2
shoot assign    "Assigning repos to products"      a
shoot backlog   "The backlog lens"                 3
shoot usage     "The usage lens"                   4
shoot velocity  "The velocity lens"                6
shoot dispatch  "Dispatching from the fleet"       1 d

echo "done."
