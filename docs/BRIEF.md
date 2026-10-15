# FanoutFuse — Build Brief (paste into Cursor / Claude)

**Goal:** Build FanoutFuse v1: a local Go daemon + CLI for **Claude Code**. It detects runaway subagent fan-out by **rate, budget and tree shape** from on-disk transcripts, **stops the whole tree** (and confirms it stopped), and writes a **one-screen post-mortem**. Read `PRD.md` (why/what, evidence) and `TRD.md` (how) in this folder first. TRD is the source of truth for design.

**Wedge (don't lose this):** a sync `PreToolUse` hook can already deny spawns, so a DIY count cap is trivial ([#83195](https://github.com/anthropics/claude-code/issues/83195)). A plain count cap is **not** the product. FanoutFuse must beat it on: burst/rate/growth/breadth/token triggers, whole-tree stop with escalation when spawns continue after Stop ([#77414](https://github.com/anthropics/claude-code/issues/77414)), and the post-mortem.

**v1 mechanics (status)**
- Spawn signal: new `~/.claude/projects/{proj}/{sessionId}/subagents/agent-*.jsonl` files (documented).
- Block step: a sync `PreToolUse` hook on `^(Task|Agent)$` returns deny when a fuse file exists (works only if **not** `async`).
- Stop step: tmux `C-x C-k` ×2 (stops background subagents in an interactive session). `claude stop <id>` works **only for background sessions**.
- PID mapping: `~/.claude/sessions/{pid}.json` is **undocumented**. Fall back to a process scan by cwd/tty.

**Constraints**
- Go 1.23+, single static binary, no cgo (`modernc.org/sqlite`). macOS + Linux only.
- **No network.** No cloud, telemetry, or HTTP server. Unix socket `0600` only.
- **Never store prompt or response text.** Parse metadata only (types, tool names, ids, flags, timestamps, usage).
- Read-only on `~/.claude/projects`. The only vendor-file write is the hook entry in `~/.claude/settings.json`, with a backup first and reversible via `uninstall`.
- Guard hook: synchronous, < 5 ms, doesn't depend on the daemon.
- Default ladder: warn → contain (fuse + guard deny) → stop background agents → **verify for 20 s** → escalate to pause/kill only when the user opts in.
- Codex adapter: interface stub only (log format unverified).

**Acceptance criteria**
1. Replays of `fixtures/69332.yaml` (2, 8, 17, 8, 16, 188 per min) and `fixtures/68619-burst.yaml` trip before the 40th synthetic agent, *earlier than a 50-count cap would*. `fanoutfuse guard` then returns a deny JSON for that session.
2. `fixtures/77361-breadth.yaml` (wide tree, depth ≤ 5) trips on `breadth_per_parent` or `spawn_rate`.
3. `fixtures/77414-post-stop.yaml` (spawns continue after stop) triggers escalation to the next enabled step, and the incident records `spawns_after_trip` per step.
4. `normal-pr-review.yaml` (5 agents) and `planned-20.yaml` (20 agents over 3 min) never trip.
5. Live Claude Code session in tmux, forced trip: fuse written, next `Agent` call denied with the FanoutFuse reason, tmux chord sent, all actions logged in `incident.actions`.
6. `fanoutfuse pause|resume <session>` sends SIGSTOP/SIGCONT to the process group. `fanoutfuse stop` stops all live sessions.
7. `fanoutfuse pm` renders the one-screen post-mortem (header, sparkline, depth histogram, top-5 branches, actions, spawns after trip, projection). `--md` export has no prompt text.
8. `fanoutfuse doctor` reports: claude root, active sessions, hook installed **and not async**, tmux, effective limits, warnings about the undocumented files.
9. `install` / `uninstall` are idempotent. Idle < 1% CPU / < 30 MB RSS. `go test ./...` passes. `golangci-lint` is clean.

**Out of scope:** UI beyond the TUI, Windows, Codex implementation, quota/statusline rules, gateway/proxy, team policy, any paid features.

**Verify early (TRD §11):** whether the transcript file is created at dispatch, meta.json fields, the tmux chord (and whether it reaches deep descendants), SIGSTOP recovery.
