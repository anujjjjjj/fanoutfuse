# FanoutFuse — paste this whole file into Cursor

You are building **FanoutFuse v1** from the attached PRD.md and TRD.md in this folder. TRD is the source of truth for how. PRD is why/what. BRIEF acceptance criteria below are the definition of done.

## Goal
Local Go daemon + CLI that detects runaway subagent fan-out in Claude Code from on-disk transcripts and stops it automatically. Detection does **not** depend on vendor hooks for timing. A sync PreToolUse guard only enforces a trip the daemon already decided.

## Wedge (do not ship a DIY count-cap only)
A sync PreToolUse count-cap already works. You must beat it with: spawn-rate / tree / budget detection, whole-tree stop with a check it actually stopped, and a one-screen post-mortem.

## Constraints
- Go 1.23+, single static binary, no cgo (`modernc.org/sqlite`). macOS + Linux only.
- No network. No cloud, telemetry, or HTTP server. Unix socket `0600` only.
- Never store prompt or response text. Metadata only.
- Read-only on `~/.claude/projects`. Only vendor write: hook entry in `~/.claude/settings.json` with backup; reversible via uninstall.
- Guard hook must be synchronous (no `"async": true`), < 5 ms, daemon-independent.
- Default action ladder: warn → contain (fuse + guard deny) → stop background agents. Pause/kill only when user opts in.
- Codex adapter: interface stub only in v1.

## Visual taste (if any UI / TUI)
Raycast-density instrument panel: charcoal glass, tight mono numerals, amber→red only when runaway. Screens: status tray, interrupt alert (Pause/Kill), post-mortem tree. At most one soft heat overlay on the spawn tree (Three UI energy). Steal from: 21st notification panel, HeroUI alert, tree-view, Raycast.com. Not a SaaS dashboard.

## Acceptance criteria
1. `fanoutfuse replay fixtures/69332.yaml` trips on spawn_rate or spawn_burst before the 40th synthetic agent; `fanoutfuse guard` then returns deny JSON.
2. Replays for #68619 / #77361 / #77414-shaped fixtures trip; trips earlier than a plain 50-count hook; records whether spawns continued after each stop step.
3. `fanoutfuse replay fixtures/normal-pr-review.yaml` (5 agents) and `planned-20.yaml` (20 over 3 min) never trip.
4. Live Claude Code in tmux forced trip: fuse written, next Agent call denied, stop chord/action logged in incident.actions.
5. `fanoutfuse pause|resume <session>` SIGSTOP/SIGCONT. `fanoutfuse stop` contains all live sessions.
6. `fanoutfuse pm` one-screen post-mortem from SQLite; `--md` exports markdown with no prompt text.
7. `fanoutfuse doctor` reports claude root, sessions, hook installed and not async, tmux, limits.
8. `fanoutfuse install` / `uninstall` idempotent. Idle < 1% CPU, < 30 MB RSS. `go test ./...` passes.

## Out of scope
Web UI beyond TUI, Windows, Codex implementation, quota/statusline rules, gateway/proxy, team policy.

## Verify early (TRD §11)
Whether subagent transcript file is created at dispatch, meta.json fields, tmux stop chord, SIGSTOP recovery.

## Repo
Create a new public or private repo (suggest `anujjjjjj/fanoutfuse` or local folder). Put PRD.md, TRD.md, BRIEF.md in `/docs`. Ship `cmd/fanoutfuse` as the single binary.

Start by scaffolding the Go module and the replay harness; get the #69332 fixture green before live Claude Code tests.
