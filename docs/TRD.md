# FanoutFuse — TRD

**Status:** Draft v0.2 · **Companion to:** [PRD.md](./PRD.md) · **Date:** 2026-10-06 (IST)

---

## 1. Constraints
- **Local only.** No cloud, no telemetry, no network calls at all in v1 (the daemon binds only to a Unix socket).
- **No reliance on vendor hooks for detection.** Sync `PreToolUse` hooks *can* deny spawns (only `async` hooks are late, per [#83195](https://github.com/anthropics/claude-code/issues/83195)), so a DIY count cap is easy. Hooks run once per call and don't remember earlier calls, so they can't see rate, budget, or tree shape. Here the hook is only the *enforcement* point for a decision the daemon has already made.
- **Wedge:** rate/budget/tree detection + stop the whole tree (with verification) + post-mortem. A plain count cap is a free extra, not the product.
- macOS + Linux for v1. Claude Code first. A Codex adapter is planned in the design but ships in v1.1.
- Single static binary. Install doesn't need root.

## 2. Stack decision: **Go**

Pick **Go** (1.23+). Reasons, in order:
1. **Anuj is a Go engineer.** The weekend build depends on speed, and Go means zero ramp-up.
2. **Fits the job:** a long-lived daemon doing file-watching, tailing many JSONL files at once, timers, and process signals. Goroutines-per-tail plus channels map onto this directly. `os/signal`, `syscall.Kill(-pgid, SIGSTOP)`, and `x/sys/unix` cover process control.
3. **One static binary, easy cross-compile** (`GOOS=darwin/linux`, arm64/amd64). No Node runtime to break when the user's agent CLI upgrades Node. Distribution is a Homebrew tap plus `go install`.
4. **Low footprint.** Easily meets the < 30 MB RSS / < 1% CPU target.

Why not the others: **Rust** gives a similar binary but costs more build time for no real gain at this scale. **Node** is the same runtime as Claude Code, so its only advantage is reuse of existing parsers, and it brings a slower start, a heavier install, and Anuj is less fluent in it.

Libraries: `fsnotify/fsnotify` (watch), `modernc.org/sqlite` (pure-Go SQLite, no cgo, keeps cross-compile easy), `BurntSushi/toml` or `pelletier/go-toml/v2`, `charmbracelet/bubbletea` + `lipgloss` (TUI), `gen2brain/beeep` or shelling out to `osascript`/`notify-send` (notifications), `shirou/gopsutil/v4` (process tree on macOS/Linux).

## 3. Architecture

```
            ┌──────────────────────── fanoutfuse daemon (user service) ───────────────────────┐
 ~/.claude  │  Adapter: claude                                                                │
 projects/  │   ├─ Watcher (fsnotify, recursive add on new dirs; 2s poll fallback)           │
 sessions/ ─┼─▶ ├─ Tailer pool (one goroutine per active JSONL; offset-tracked, metadata only)│
            │   └─ SessionResolver (sessionId ⇄ pid ⇄ tty/tmux pane)                        │
 ~/.codex   │  Adapter: codex (v1.1) ── same Event interface                                   │
 sessions/ ─┼─▶                                                                                │
            │           │ Event{kind: SpawnObserved|AgentStart|AgentEnd|Usage, sid, aid, ...}│
            │           ▼                                                                     │
            │  Tree store (in-mem per session) ──▶ Rule engine (sliding windows) ──▶ Verdict │
            │           │                                   │                            │    │
            │           ▼                                   ▼                            ▼    │
            │  SQLite (incidents, nodes, buckets)    Notifier              Actuator ladder  │
            │                                                          (fuse→chord→STOP→KILL)│
            │  Control API: unix socket ~/.fanoutfuse/ctl.sock (JSON-RPC) ◀── CLI / TUI      │
            └───────────────────────────────────────────────────────────────────────────────┘
                     ▲
  PreToolUse hook ───┘  `fanoutfuse guard` (sync, <5 ms): stat fuse file → deny/allow
```

Processes: one daemon (`fanoutfuse run`). The CLI talks to it over the socket. The hook binary is the same executable (`fanoutfuse guard`). It doesn't connect to the daemon. It only `stat`s a file, so it stays fast and still works when the daemon is down.

## 4. Detection without late hooks

### 4.1 Data sources (Claude Code)
Verified from the [Claude Code subagent docs](https://code.claude.com/docs/en/sub-agents) and [claude-session-visualizer README](https://github.com/anaypaul/claude-session-visualizer/blob/main/README.md):

| Path | Use |
|---|---|
| `~/.claude/projects/{proj}/{sessionId}.jsonl` | Main transcript: `tool_use` blocks with `name` ∈ {`Agent`,`Task`}, `usage` on assistant messages |
| `~/.claude/projects/{proj}/{sessionId}/subagents/agent-{agentId}.jsonl` | **One file per subagent.** File creation = spawn signal. Inner `Agent` tool_use = this agent is spawning |
| `~/.claude/projects/{proj}/{sessionId}/subagents/agent-{agentId}.meta.json` | Agent metadata (parent/type). **Field names need checking against a live install** |
| `~/.claude/sessions/{pid}.json` | Live session registry: PID ⇄ sessionId ⇄ cwd. **Undocumented** (seen in community tools). Fallback: process scan matching cwd/tty |
| `$CLAUDE_CONFIG_DIR` | Use it as the root if set (multi-profile, as ccburn does) |

### 4.2 Signals (cheapest first)
1. **S1: file-create events** in any `subagents/` dir (fsnotify `Create`). Doesn't depend on the schema, gives a timestamp at the OS level, and fires before the agent's first model response finishes. **This is the primary signal.** It survives transcript-format changes.
2. **S2: `tool_use` name=Agent/Task** lines seen while tailing (parent and subagent files). Gives *intent to spawn* plus parent attribution and `run_in_background`, `subagent_type`. It can arrive before S1.
3. **S3: `usage` blocks** (input/output/cache tokens) per agent. Feeds spend-velocity rules and the post-mortem.
4. **S4 (v1.2): statusline `rate_limits`** JSON (pattern from [ccburn](https://github.com/JuanjoFuchs/ccburn)). Gives quota % velocity.

Fallback: if fsnotify drops events (macOS FSEvents coalescing, inotify watch limits), a 2 s poll `ReadDir`s the active sessions' `subagents/` dirs and diffs the counts. Watches are added only under *active* sessions (from `sessions/*.json` + mtime < 10 min), so we stay well under the inotify limits.

### 4.3 Tree reconstruction
- Node = `agentId`. Parent comes from (a) meta.json if it has a parent field, else (b) matching the parent transcript's `tool_use.id` to the tool_result that carries the child agentId, else (c) "unknown parent, depth ≥ 1".
- Depth = parent depth + 1. Root = main session (depth 0).
- Kept in memory per session, and flushed to SQLite on trip and every 30 s.

### 4.4 Rule engine
Sliding windows over spawn timestamps per session (a ring buffer of `time.Time`), evaluated on every event and on a 1 s ticker:

| Rule | Default warn | Default trip | Rationale (from evidence) |
|---|---|---|---|
| `spawn_rate` (spawns / 60 s) | 15 | 30 | #69332 had 16–17/min before the 188/min spike. Normal use is < 10/min |
| `spawn_burst` (spawns / 10 s) | 8 | 15 | Catches the exponential knee early |
| `total_spawns` per session | 40 | 80 | Vendor total cap is gone (v2.1.224). The old default was 200 |
| `max_depth` | 3 | 4 | Vendor default depth is 3. Deeper than that means overrides or a bug |
| `running_concurrent` | 15 | 25 | Ultracode is exempt from the vendor cap of 20 |
| `tokens_per_min` (all agents) | off | off | Opt-in. Plan-dependent |
| `growth_factor` (spawns in last 30 s ÷ prev 30 s) | ≥3 & n≥10 | ≥4 & n≥20 | Exponential signature |
| `breadth_per_parent` (direct children of one agent) | 6 | 10 | #77361: 877 agents while staying within the depth limit |

A verdict is `Warn` or `Trip{rule, value, threshold}`. Hysteresis: once warned, don't re-warn for 60 s. A trip is latched until the user resets it.

## 5. Kill / pause strategy

### 5.1 Ladder (configurable; default stops at step 2 + verify)
| Step | Action | Effect | Reversible |
|---|---|---|---|
| 0 | **Warn** | Notification + bell | n/a |
| 1 | **Contain**: write `~/.fanoutfuse/fuse/{sessionId}` (+ global `ALL`) | The sync `PreToolUse` guard hook denies `Agent`/`Task` with a reason telling the model to finish the work itself. Running agents keep going; no new spawns | Yes (`resume`) |
| 2 | **Stop background agents**: if the session's TTY is a tmux pane → `tmux send-keys -t <pane> C-x C-k` twice within 3 s | Uses Claude Code's built-in "Stop all running background subagents" ([interactive-mode docs](https://code.claude.com/docs/en/interactive-mode)). Keeps the main session | Main session kept |
| 3 | **Pause**: `kill(-pgid, SIGSTOP)` on the session's process group | Freezes all requests and spawns right away. `resume` → `SIGCONT` | Yes, but in-flight streams may time out |
| 2b | **Stop background session** (only if the session is a Claude Code *background session*): `claude stop <id>` | Per the [CLI reference](https://code.claude.com/docs/en/cli-reference), this stops background sessions only, not subagents inside a foreground session | Session can be resumed |
| 4 | **Kill**: `SIGINT` → wait 3 s → `SIGTERM` to pgid → wait 5 s → `SIGKILL` | Same as `/exit`. Subagents in the same process go down with it (#69332 confirms subagents ended at exit: "in-process state lost") | No (the session can be `--resume`d) |

Notes:
- Claude Code subagents run **inside the main process** (#69332: "in-process state lost"). There's no per-subagent PID, so per-agent kill isn't possible from outside. Step 2 is the closest thing to it. Bash children spawned by agents are in the process tree, and step 3/4 handles them through the pgid.
- **Verify after every step.** Watch for new `agent-*.jsonl` files for `escalate_after` (default 20 s). If spawns continue, go to the next enabled step. [#77414](https://github.com/anthropics/claude-code/issues/77414) recorded ~300 more agents *after* Stop, so a stop isn't trusted until it's confirmed. Record `spawns_after_trip` per step.
- Steps 1 and 2 are the default because they **keep the user's main session**, which #69332 calls out as the missing control ("no graceful stop short of killing the whole session").
- Not in tmux: step 2 is skipped and the notification offers step 3/4 instead. Later work: AppleScript key injection for iTerm/Ghostty (opt-in).

### 5.2 Guard hook (enforcement point)
Installed by `fanoutfuse install` into `~/.claude/settings.json`:
```json
{"hooks":{"PreToolUse":[{"matcher":"^(Task|Agent)$",
  "hooks":[{"type":"command","command":"fanoutfuse guard"}]}]}}
```
- Must **not** set `"async": true`. Per the maintainer's reply on #83195, async hooks can't block. `doctor` checks for this and refuses to call the setup healthy if it's async.
- `guard` reads stdin JSON (session_id), `stat`s the fuse files, and either exits 0 or prints `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"FanoutFuse tripped (spawn_rate 34/min > 30). Do not spawn subagents; complete remaining work directly."}}`. Target < 5 ms, no daemon dependency.
- Optional (off by default): `guard` also enforces the static `total_spawns`/`max_depth` limits by counting files in `subagents/`. This is the same as a DIY count-cap hook, included as a backup in case the daemon is down. It isn't the wedge.

### 5.3 Per-platform
| | Claude Code (v1) | Codex (v1.1) |
|---|---|---|
| Spawn signal | `subagents/agent-*.jsonl` create + `Agent`/`Task` tool_use | Rollout JSONL spawn-agent events under `~/.codex/sessions/` (**path/event names to verify**). MultiAgentV2 payloads are encrypted ([openai/codex#28058](https://github.com/openai/codex/issues/28058)), so expect metadata only |
| Contain | Sync PreToolUse guard + fuse file | Codex hook support is still to be checked. Fallback: no contain step, go straight to step 3. Recommend `agents.max_concurrent_threads_per_session` in `doctor` |
| Graceful stop | tmux `C-x C-k` ×2 | Codex IDE has "stop all active subagents". CLI: send `Esc`/interrupt via tmux (verify) |
| Pause / Kill | pgid SIGSTOP / SIGINT→TERM→KILL | same |
| macOS / Linux diff | Process discovery: `gopsutil` (libproc vs /proc). Notify: `osascript` vs `notify-send`. Service: launchd plist vs systemd `--user` unit | same |
| Windows (later) | Job Objects for tree kill, `NtSuspendProcess` for pause, no tmux | same |

## 6. Configuration

`~/.config/fanoutfuse/config.toml`, overridden by `<repo>/.fanoutfuse.toml` (nearest ancestor of the session cwd), overridden by env `FANOUTFUSE_*`:

```toml
[general]
action_ladder   = ["warn", "contain", "stop_background"]   # add "pause" / "kill" to escalate
escalate_after  = "20s"     # if spawns continue after contain, move to next step
notify          = true
bell            = true
store_prompts   = false     # never true by default; metadata only

[limits]                     # per session
spawn_rate_per_min   = { warn = 15, trip = 30 }
spawn_burst_per_10s  = { warn = 8,  trip = 15 }
total_spawns         = { warn = 40, trip = 80 }
max_depth            = { warn = 3,  trip = 4 }
running_concurrent   = { warn = 15, trip = 25 }
breadth_per_parent   = { warn = 6,  trip = 10 }
growth_factor_30s    = { warn = 3,  trip = 4, min_n_warn = 10, min_n_trip = 20 }
tokens_per_min       = { warn = 0,  trip = 0 }   # 0 = off

[adapters.claude]
enabled = true
root    = "~/.claude"            # honours $CLAUDE_CONFIG_DIR
[adapters.codex]
enabled = false                  # v1.1
root    = "~/.codex"

[cooldown]
after_resume = "10m"             # limits ×2 for this long after a manual resume
```

The CLI prints the effective config with where each value came from: `fanoutfuse config --explain`.

## 7. One-screen post-mortem data model

SQLite at `~/.fanoutfuse/fanoutfuse.db` (WAL). Metadata only.

```sql
CREATE TABLE incident (
  id            TEXT PRIMARY KEY,       -- ulid
  platform      TEXT NOT NULL,          -- claude|codex
  session_id    TEXT NOT NULL,
  project_path  TEXT,                   -- cwd
  cli_version   TEXT,
  started_at    INTEGER,                -- first spawn in window (unix ms)
  warned_at     INTEGER,
  tripped_at    INTEGER,
  rule          TEXT,                   -- spawn_rate|total_spawns|...
  rule_value    REAL, rule_threshold REAL,
  actions       TEXT,                   -- JSON [{step,at,ok,err}]
  contained_at  INTEGER,                -- last new spawn observed after trip
  spawns_total  INTEGER, spawns_after_trip INTEGER,
  max_depth     INTEGER,
  tokens_in INTEGER, tokens_out INTEGER, tokens_cache INTEGER,
  est_cost_usd  REAL,                   -- API-equivalent, price table versioned
  projected_spawns_if_unstopped INTEGER,-- fit growth curve at trip time, +5 min
  resolution    TEXT                    -- resumed|killed|ignored|false_positive
);
CREATE TABLE agent_node (
  incident_id TEXT, agent_id TEXT, parent_id TEXT, depth INTEGER,
  subagent_type TEXT, model TEXT, background INTEGER,
  created_at INTEGER, last_event_at INTEGER,
  spawn_calls INTEGER, bytes INTEGER,
  tokens_in INTEGER, tokens_out INTEGER, tokens_cache INTEGER,
  status TEXT,                            -- running|done|stopped|unknown
  PRIMARY KEY (incident_id, agent_id)
);
CREATE TABLE spawn_bucket (               -- 10 s buckets for sparkline
  incident_id TEXT, bucket_start INTEGER, spawns INTEGER, tokens INTEGER,
  PRIMARY KEY (incident_id, bucket_start)
);
```

Screen layout (bubbletea, fits in 100×30):
```
FanoutFuse · incident 01J… · claude 2.1.288 · ~/code/api · session a1b2
TRIPPED spawn_rate 34/min > 30 at 13:34:12 IST · time-to-trip 3.1s · contained 13:34:19
Spawns ▁▁▂▃▂▃▅█▇▁▁  total 47 (after trip: 2) · depth max 4 · ~1.9M tok · ~$11.40 API-eq
Projected if unstopped (+5m): ~600 agents
Tree (by depth)            Loudest branches              tokens   kids
 d0 main  1                 agent-7f2 general-purpose     310k      9
 d1      2                  agent-c19 general-purpose     244k      7
 d2     10                  …
 d3     29
 d4      5
Actions: contain ✓ 13:34:12 · stop_background ✓ 13:34:15 · pause – · kill –
[r]esume  [k]ill session  [m]arkdown export  [f]alse positive  [q]uit
```
`fanoutfuse pm --md` renders the same data as markdown that can be pasted into a GitHub issue. Agent ids are kept, paths are optional, and prompts are never included.

## 8. Privacy
- The daemon parses only: line `type`, tool `name`, `id`, selected input flags (`run_in_background`, `subagent_type`, `model`), timestamps, and `usage` numbers. **Prompt/response text is never decoded into the store.** `store_prompts=false` is hard-wired in v1.
- No sockets other than a `0600` Unix socket. No HTTP listener in v1. No update checks.
- DB and fuse dir are `0700` under `~/.fanoutfuse/`. `fanoutfuse purge` deletes everything.
- Transcripts are read-only. FanoutFuse never writes to `~/.claude/projects`. The only file it writes in the vendor's space is the hook entry in `settings.json`, with a backup first and `fanoutfuse uninstall` to remove it.

## 9. Testing
- **Replay harness:** `fanoutfuse replay fixtures/69332.yaml` creates fake session dirs and writes `agent-*.jsonl` files on the recorded per-minute curve (2, 8, 17, 8, 16, 188) and the 339-in-10-min curve from the Reddit thread. Assert: trip before 40 agents, guard denies after trip, buckets are correct.
- **Normal-use fixtures:** 5-agent PR review, 20-agent planned fan-out, workflow-style 50-agent batch (expect warn, configurable trip).
- **Live smoke test (manual, cheap model):** a prompt that asks for nested background agents with `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=5`. Check S1 latency, guard deny, tmux chord, and SIGSTOP/SIGCONT recovery.
- Unit: rule windows, tree attribution, guard latency (< 5 ms p99).

## 10. Package layout (suggested)
```
cmd/fanoutfuse/        main: run|guard|status|stop|pause|resume|pm|doctor|install|replay
internal/adapter/claude/  watcher, tailer, resolver, schema probes
internal/adapter/codex/   (v1.1)
internal/tree/         in-mem session trees
internal/rules/        windows, verdicts, config
internal/actuator/     fuse, tmux, signals (unix), notify
internal/store/        sqlite, migrations
internal/tui/          post-mortem screen
internal/ctl/          unix-socket JSON-RPC
fixtures/              replay curves
```

## 11. Unknowns to verify before / during the build
1. Whether `agent-*.jsonl` is created at dispatch or at the first write (sets the S1 latency floor).
2. `meta.json` field names for the parent / agent type.
3. Whether `tmux send-keys C-x C-k` registers as the chord in Claude Code's TUI (Kitty keyboard protocol could get in the way).
4. Whether Claude Code survives SIGSTOP > 60 s mid-stream.
5. Codex: rollout path / spawn event names and whether Codex has a blocking pre-tool hook.
