#!/bin/sh
# Real-run proof that the FanoutFuse plugin refuses Agent, Workflow and
# ultracode spawns and stops what is running. Runs three short headless Claude Code
# sessions against a throwaway project and keeps the logs.
#
#   sh scripts/proof/prove.sh            # all three runs
#   sh scripts/proof/prove.sh workflow   # one run: agent, workflow or ultracode
#
# Needs: Claude Code 2.1.300+ signed in (`claude` works), a plan with dynamic
# workflows (Pro needs "enableWorkflows": true, passed per run below, nothing
# global changes). The ultracode run also needs a model with xhigh effort
# (Opus); it uses a few percent of the 5-hour window.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
plugin=$(cd "$here/../../plugin" && pwd)
out=${OUT:-"$here/../../docs/evidence/$(date +%Y%m%d-%H%M%S)"}
which=${1:-all}
mkdir -p "$out"
claude --version > "$out/claude-version.txt"

run() { # name model effort trip prompt
  name=$1; model=$2; effort=$3; trip=$4; prompt=$5
  dir="$out/$name"; proj="$dir/project"
  mkdir -p "$proj"
  printf 'started %s\n' "$(date -u +%FT%TZ)" > "$dir/meta.txt"
  ( cd "$proj" && FANOUTFUSE_HOME="$dir/fanoutfuse-home" FANOUTFUSE_TOTAL_SPAWNS_TRIP=$trip \
      claude -p "$(cat "$here/$prompt")" \
        --model "$model" $effort \
        --plugin-dir "$plugin" \
        --settings '{"enableWorkflows":true}' \
        --permission-mode bypassPermissions \
        --output-format stream-json --verbose \
        > "$dir/stream.jsonl" 2> "$dir/stderr.txt" ) || true
  printf 'ended %s\ntrip total_spawns=%s\n' "$(date -u +%FT%TZ)" "$trip" >> "$dir/meta.txt"
  sh "$here/summarize.sh" "$dir" | tee "$dir/summary.txt"
}

[ "$which" = all ] || [ "$which" = agent ] && run agent sonnet "" 2 agent-prompt.txt
[ "$which" = all ] || [ "$which" = workflow ] && run workflow sonnet "" 3 workflow-prompt.txt
[ "$which" = all ] || [ "$which" = ultracode ] && run ultracode opus "--effort ultracode" 2 ultracode-prompt.txt
echo "logs in $out"
