#!/bin/sh
# Does a settings SubagentStart hook that exits 2 stop a subagent?
# Loads a throwaway plugin whose SubagentStart hook always exits 2, asks for
# one Agent-tool subagent and one two-agent Workflow, and checks whether the
# subagents still answered. Claude Code's hooks reference says SubagentStart
# cannot block; this checks it on your build.
#
#   sh scripts/proof/subagentstart-exit2.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
out=${OUT:-"$here/../../docs/evidence/$(date +%Y%m%d-%H%M%S)"}/subagentstart-exit2
tmp=$(mktemp -d)
mkdir -p "$out" "$tmp/plugin/.claude-plugin" "$tmp/plugin/hooks" "$tmp/project"
printf '{"name":"exit2-probe","version":"0.0.0"}\n' > "$tmp/plugin/.claude-plugin/plugin.json"
cat > "$tmp/plugin/hooks/hooks.json" <<'JSON'
{"hooks":{"SubagentStart":[{"hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}/hooks/exit2.sh\""}]}]}}
JSON
cat > "$tmp/plugin/hooks/exit2.sh" <<SH
#!/bin/sh
printf '%s SubagentStart fired, exiting 2\n' "\$(date -u +%FT%TZ)" >> "$out/hook.log"
echo "exit2-probe: refusing subagent (exit 2)" >&2
exit 2
SH
chmod +x "$tmp/plugin/hooks/exit2.sh"
claude --version > "$out/claude-version.txt"

ask() { # name prompt
  ( cd "$tmp/project" && claude -p "$2" --model sonnet --plugin-dir "$tmp/plugin" \
      --settings '{"enableWorkflows":true}' --permission-mode bypassPermissions \
      --output-format stream-json --verbose \
      > "$tmp/$1.jsonl" 2> "$out/$1.stderr.txt" < /dev/null ) || true
}
ask agent 'Use the Agent tool once with model "haiku" and the prompt "Reply with only PONG-7731." Then tell me exactly what it returned.'
ask workflow "Call the Workflow tool once with this exact script: export const meta = { name: 'exit2-probe', description: 'two tiny agents', phases: [{ title: 'Go' }] }
phase('Go')
return await parallel([() => agent('Reply with only ALPHA-1.', { label: 'a', model: 'haiku' }), () => agent('Reply with only BETA-2.', { label: 'b', model: 'haiku' })])
Then run the Bash command 'sleep 20' in the background and, when the workflow finishes, report what each agent returned."

answered() { # file marker: some JSON string in the run is exactly the marker, i.e. the subagent's own reply
  grep -q "\":\"$2\"" "$1" && echo yes || echo no
}
{
  echo "SubagentStart hook fired: $(grep -c 'SubagentStart fired' "$out/hook.log" 2>/dev/null || echo 0) times, exiting 2 each time"
  echo "Agent subagent still answered PONG-7731: $(answered "$tmp/agent.jsonl" PONG-7731)"
  echo "Workflow agent a still answered ALPHA-1: $(answered "$tmp/workflow.jsonl" ALPHA-1)"
  echo "Workflow agent b still answered BETA-2:  $(answered "$tmp/workflow.jsonl" BETA-2)"
  echo "subagent_stats (agent run): $(grep -o '"subagent_stats":{[^}]*}' "$tmp/agent.jsonl" | tail -1)"
} | tee "$out/summary.txt"
[ -n "${KEEP:-}" ] && echo "kept $tmp" || rm -rf "$tmp"
