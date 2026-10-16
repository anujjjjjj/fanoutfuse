# 90-second demo script

Setup before recording: a fresh terminal, font size large, binary on PATH.

```sh
export FANOUTFUSE_HOME="$(mktemp -d)/state"
```

## 0:00–0:15 — The problem
Say: "When an agent session fans out, subagents spawn subagents. Fixture 69332 is a real-shaped incident: 239 spawns, 188 in the final minute. A flat 50-agent cap only reacts at agent 50."

## 0:15–0:45 — Replay the trip
```sh
fanoutfuse replay fixtures/69332.yaml
```
Point at: `TRIP rule=spawn_burst ... at agent 25` and `count-cap@50 would trip at agent 50 (25 earlier)`. Say: "Virtual time, so a four-minute incident replays in under a second. It trips on burst rate, not on a count."

## 0:45–1:05 — Live guard deny
```sh
echo '{"session_id":"69332","tool_name":"Agent","hook_event_name":"PreToolUse"}' | fanoutfuse guard
```
Point at `"permissionDecision":"deny"` and the reason. Say: "This is the PreToolUse hook. Once the fuse is set, every new Agent or Task call is denied with the reason, even if the daemon is down."

## 1:05–1:25 — Post-mortem
```sh
fanoutfuse pm
```
Point at spawn sparkline, depth tree, top branches, and the action timeline.

## 1:25–1:30 — Close
Say: "One Go binary, local only, no telemetry. `brew install anujjjjjj/tap/fanoutfuse`."
