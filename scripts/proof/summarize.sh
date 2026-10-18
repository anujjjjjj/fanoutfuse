#!/bin/sh
# Summarizes one proof run directory: spawns refused, stops issued, verdict.
set -eu
dir=$1
s="$dir/stream.jsonl"
echo "== $(basename "$dir")"
echo "workflow tool calls:      $(grep -o '"name":"Workflow"' "$s" | wc -l | tr -d ' ')"
echo "plugin toasts:            $(grep -c '"subtype":"ui_toast","plugin":"fanoutfuse"' "$s" || true)"
echo "task stopped events:      $(grep -c '"status":"stopped"' "$s" || true)"
inc=$(ls "$dir"/fanoutfuse-home/incidents/*.json 2>/dev/null | head -1 || true)
if [ -n "$inc" ]; then
  echo "incident:                 $inc"
  grep -E '"(reason|stopVerified|abortedTurn)"' "$inc" | sed 's/^ */  /'
  echo "  counts (started vs refused per spawn path):"
  sed -n '/"counts"/,/"metrics"/p' "$inc" | grep -E '"(agent|workflow|fork|tool|spawned|denied)"' | tr -d ' {},' | paste -sd' ' - | sed 's/^/    /'
  echo "  stops:"
  grep -E '"(id|attempt|ok)"' "$inc" | tr -d ' ,' | paste -d' ' - - - | sed 's/^/    /'
else
  echo "incident:                 none (the fuse did not trip)"
fi
