# FanoutFuse — PRD

**Status:** Draft v0.2 (Product evidence pack folded in) · **Owner:** Anuj Kabra · **Date:** 2026-10-06 (IST)
**One-liner:** A local kill switch that watches the *whole* subagent tree of a coding agent (Claude Code first, Codex next). It trips on spawn **rate, budget and tree shape**, stops the entire tree, and leaves you a one-screen post-mortem.

---

## 1. Problem

Claude Code subagents can spawn their own subagents. When a `general-purpose` subagent (which keeps the `Agent` tool) fans out, its children can fan out too. A request for 5 agents becomes hundreds within minutes. Users find out from indirect signs: rate-limit errors, a frozen machine, or a 5-hour / weekly quota that is gone. Stopping it is unreliable: by the time a human reacts, dozens more agents have launched, and the built-in Stop doesn't always cancel deeper descendants.

### 1.1 Evidence: Claude Code (all links checked 2026-10-06)

| # | Source | What it shows |
|---|---|---|
| E1 | [claude-code#68619](https://github.com/anthropics/claude-code/issues/68619) (open, 22 reactions, 34 comments) | "consumed an entire **Pro Max 20x** plan 5-hour session token limit in **under 5 minutes (4 million tokens consumed)**". Over 50% of a weekly Max 20x limit burned by recursive spawning. Interrupting is "the only way to stop a runaway recursive chain", and it throws away all subagent results. |
| E2 | [claude-code#68110](https://github.com/anthropics/claude-code/issues/68110) (open, 12 reactions) | One research `Agent` call → **48+ background agents**. "by the time the user sees them in the agent list and manually stops them, dozens have already launched and consumed tokens." |
| E3 | [claude-code#69332](https://github.com/anthropics/claude-code/issues/69332) (closed as stale, not fixed) | ~242 transcripts / 251 spawn calls. Per-minute curve 2 → 8 → 17 → 8 → 16 → **188**. "No graceful stop short of killing the whole session." A Windows repro in the same thread: 428 agent files, about the entire 5-hour window plus ~16% of the weekly limit (measured with ccburn). |
| E4 | [claude-code#77361](https://github.com/anthropics/claude-code/issues/77361) (closed) | **877 agents** from a nested code review **even though the 5-level depth limit held**. "the breadth still produced an unreasonable number of model calls." Asks for total-descendant and token/cost budgets. |
| E5 | [claude-code#77414](https://github.com/anthropics/claude-code/issues/77414) (closed) | **After Stop**, "approximately **300 additional Agent metadata records** were created and 256 further nested Agent tool calls." Windows froze at 100% CPU and needed a **hard reboot** (Kernel-Power 41). About 583 records in the incident window. |
| E6 | [claude-code#80988](https://github.com/anthropics/claude-code/issues/80988) (open, 91 reactions) | A user found that Anthropic **added "Do not call the AgentTool unless the user requested it" to the system prompt** (v2.1.219, Opus 5 only, no opt-out). The vendor is already working on this problem, and users dislike blunt fixes that override their delegation policy. |
| E7 | [claude-code#83195](https://github.com/anthropics/claude-code/issues/83195) (closed) | **Correction from v0.1:** a maintainer showed that **synchronous `PreToolUse:Agent` hooks do block** subagent dispatch. Only `"async": true` hooks run too late. So a DIY count-cap hook works today. |
| E8 | [v2.1.224 release notes](https://newreleases.io/project/github/anthropics/claude-code/release/v2.1.224) (2026-08-07), [env-var history](https://changelogs.core-directive.com/reference/env/claude-code-max-subagents-per-session) | "Removed the 200-subagent-per-session spawn cap." `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` does nothing now. |
| E9 | [Subagent docs](https://code.claude.com/docs/en/sub-agents) | Built-in limits now: depth 3 (`CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`), concurrency 20 (`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS`). "There's no limit on the total number of subagents Claude can spawn over a session." Ultracode is exempt from the concurrency limit. Workflows and teams have their own limits. |
| E10 | Reddit: [r/ClaudeAI, 339 sub-agents](https://www.reddit.com/r/ClaudeAI/comments/1u5d02w/til_claude_code_spawned_339_subagents_from_a/), [r/ClaudeCode, 428 agents / Max 20x](https://www.reddit.com/r/ClaudeCode/comments/1udo9rm/claude_code_just_burned_almost_my_full_max_20x/) | Same pattern in community threads. Commenters ask for "a hard ceiling plus somewhere to actually see what's running". |

### 1.2 Evidence: Codex

| # | Source | What it shows |
|---|---|---|
| C1 | [openai/codex#9748](https://github.com/openai/codex/issues/9748) (closed, 18 reactions, 31 comments) | Launching ~6 concurrent subagents "instantly drains entire **Pro** plan usage quota". |
| C2 | [openai/codex#28058](https://github.com/openai/codex/issues/28058) (open, 145 reactions) | MultiAgentV2 message payloads are now **encrypted**, which "remove[s] readable task audit trail". Post-mortems from Codex logs may only see metadata. |
| C3 | [Codex subagents docs](https://developers.openai.com/codex/subagents) | Subagents are on by default. `agents.max_concurrent_threads_per_session` caps concurrency only. |

> We do **not** headline the unverified "Codex $78k" HN claim. (Separately, the ~$78.8K MRR figure below is **LLM Gateway's** TrustMRR listing, not a Codex incident.)

### 1.3 What already exists, and the gap

**Vendor and DIY controls cap *counts*:** depth (3), concurrency (20), and a sync PreToolUse hook that refuses after N spawns. That's useful, but the evidence shows where it fails:
- **Breadth inside the depth limit** (E4: 877 agents at depth ≤ 5).
- **Speed:** spawns outrun human reaction (E1 <5 min, E2, E3 188/min).
- **Stop doesn't reach the whole tree** (E5: ~300 more after Stop).
- **No budget view:** a count cap can't tell 80 cheap agents from 20 expensive ones (E1 4M tokens).
- **Exemptions:** ultracode, workflows, resumes (E9).
- **Opaque afterwards:** no record of what ran, why, or what it cost (C2 makes this worse on Codex).

**Monitors observe or alert but don't stop the tree automatically** (stars as of 2026-10-06):

| Tool | ★ | What it does | Auto-stop? |
|---|---|---|---|
| [hoangsonww/Claude-Code-Agent-Monitor](https://github.com/hoangsonww/Claude-Code-Agent-Monitor) | ~1045 | Dashboard built on hooks, Claude Code + Codex, notifications | **Alerts only** |
| [JuanjoFuchs/ccburn](https://github.com/JuanjoFuchs/ccburn) | 96 | Quota burn-up TUI, "time to limit" | No |
| [ShreyPaharia/octomux](https://github.com/ShreyPaharia/octomux) | 23 | One agent per tmux window/worktree, manual kill | No (prevents fan-out by design) |
| [markiv25/Viz](https://github.com/markiv25/Viz) | 1 | Subagent view, manual kill | View-only + manual |
| claude-watch ([tarikguney](https://github.com/tarikguney/claude-watch) 19★, [odedha-dr](https://github.com/odedha-dr/claude-watch), [shuxuecode](https://github.com/shuxuecode/claude-watch), [NirDiamant](https://github.com/NirDiamant/claude-watch)) | — | Live dashboards | View-only |
| [anaypaul/claude-session-visualizer](https://github.com/anaypaul/claude-session-visualizer) | 1 | Execution trees from JSONL | View-only |
| [LLM Gateway](https://trustmrr.com/startup/llm-gateway) | — | API gateway with cost tracking. TrustMRR ~$78.8K MRR (stale since 2026-09-07) | Cost control for **API keys**. Can't see subscription quota traffic and needs cloud routing |

### 1.4 Wedge (what we must be clearly better at)
A simple count cap is **not** the product. Anyone can write one in a 10-line sync hook today (E7). FanoutFuse's wedge:
1. **Rate / budget / tree detection.** Triggers on spawns per 10 s and per minute, growth factor, breadth per parent, tokens per minute, and quota velocity. Built from transcripts, independent of hooks.
2. **Stop the whole tree.** Block new spawns, stop background agents, then confirm the tree actually stopped. If spawns keep coming (E5), escalate to pause or kill the process group.
3. **One-screen post-mortem.** Tree, timeline, spend, rule, actions, and "projected if unstopped".

## 2. Who
- **Primary: power users on flat-rate plans** (Claude Max 5x/20x, Pro; ChatGPT Pro with Codex) who run background or multi-agent sessions. What they lose is **quota time, not cash** (E1, E3, C1).
- **Secondary: API-key users**, who lose real money.
- **Tertiary (later): small teams** that want a shared policy and post-mortems without a gateway. This is the most likely group to pay (§7).
- **Not for (v1):** enterprises that need central admin or a cloud console.

## 3. Goals / Non-goals
**Goals**
1. Detect runaway fan-out within **≤ 10 s** of crossing a rate/budget/tree threshold, from on-disk data and not hooks.
2. **Stop the whole tree** automatically: block new spawns → stop background agents → *verify that spawning stopped* → escalate.
3. One-screen **post-mortem** for every trip, exportable to a GitHub issue.
4. **Local only:** no cloud, no telemetry, metadata only.
5. Install in ≤ 2 min. Defaults cause ≤ 1 false trip per 50 power-user sessions.

**Non-goals (v1)**
- Being "just a count cap" (users already have hooks and vendor env vars, and we work alongside them).
- A general cost dashboard (ccusage/ccburn), an API proxy or gateway.
- Windows, Cursor, Gemini CLI. Codex adapter ships in v1.1.
- Changing agent behaviour or prompts (E6 shows users dislike this).

## 4. User journeys

### J1 — Alert → stop tree → decide
1. Anuj runs `claude` in tmux. The `fanoutfuse` user daemon is watching.
2. A research prompt fans out. New `subagents/agent-*.jsonl` files appear: 8 in 10 s, and the growth factor is ×4 over the last 30 s.
3. **Warn:** desktop notification + bell: "session a1b2: 25 agents/60 s, ×4 growth, 1.1M tok/min. [Stop tree] [Ignore 10m]".
4. **Trip** (automatic): (a) a fuse file is written, so the sync `PreToolUse` guard denies every new `Agent`/`Task` call; (b) in tmux, the daemon sends Claude Code's "stop all background subagents" chord (Ctrl+X Ctrl+K ×2); (c) **verification**: the daemon watches for new transcript files for 20 s. If spawns continue (as in E5), it escalates to pause (SIGSTOP) or kill, depending on config.
5. Notification: "Tree stopped: 31 agents stopped, 2 spawned after trip, main session kept. [Resume] [Kill] [Post-mortem]".

### J2 — Manual panic button
`fanoutfuse stop` runs the full stop on every live agent session. `pause` / `resume <session>` sends SIGSTOP/SIGCONT to the process group.

### J3 — Post-mortem
`fanoutfuse pm` shows one screen: rule and time-to-trip, a 10 s spawn sparkline, a depth histogram, the 5 loudest branches by tokens and children, actions with timestamps, spawns after trip, estimated tokens/$ (API-equivalent), and projected agents if unstopped. `--md` gives markdown for an issue, with no prompt text.

### J4 — Tuning / doctor
`fanoutfuse doctor` checks: transcripts found, guard hook installed **and synchronous**, tmux present, CLI version, the built-in limits that are set, and warns when the undocumented files it relies on are missing or have changed shape.

## 5. Success metrics
| Metric | Target (v1, dogfood + 20 beta users) |
|---|---|
| Detection latency (threshold → trip) | p95 ≤ 5 s, max ≤ 10 s |
| Tree-stop latency (trip → no new transcript files) | p95 ≤ 15 s, including escalation |
| Agents spawned after trip | ≤ 5 (vs ~300 in E5) |
| **Better than a count cap** | On replays of E1/E3/E4/E5 curves, trips **before** a 50-count hook would, and catches the E4 (breadth) and E5 (post-stop) cases that a count cap misses or only partly contains |
| False trips | ≤ 1 / 50 normal sessions, each fixable with one key |
| Install → protected | ≤ 2 min |
| Overhead | < 1% CPU idle, < 30 MB RSS |
| Adoption | 100 ★ / 20 weekly active installs in 30 days. ≥ 3 "it saved me" reports. ≥ 5 teams asking for shared policy (tests the paid tier) |

## 6. Scope

### Weekend v1: Claude Code, macOS + Linux
**Mechanics we rely on (status):**
- **Transcripts:** `~/.claude/projects/{proj}/{sessionId}/subagents/agent-{agentId}.jsonl`, one file per subagent ([documented](https://code.claude.com/docs/en/sub-agents)). File creation is the primary spawn signal.
- **Enforcement:** a sync `PreToolUse` hook on `Agent`/`Task` **can deny** (E7). Used as the block step, *not* as the detector.
- **Stop:** Ctrl+X Ctrl+K stops background subagents in an interactive session ([documented](https://code.claude.com/docs/en/interactive-mode)). The `claude stop <id>` CLI **only stops background sessions** ([CLI reference](https://code.claude.com/docs/en/cli-reference)), not subagents inside a foreground session. Stop is not always complete (E5), so we verify and escalate.
- **PID mapping:** `~/.claude/sessions/{pid}.json` is **undocumented** (seen in community tools). We use it when present and fall back to scanning processes by cwd/tty.

**Features:**
- Go single-binary daemon + CLI (`run|status|stop|pause|resume|pm|doctor|install|replay`).
- Rules: burst (10 s), rate (60 s), growth factor, breadth per parent, total, depth, tokens/min (opt-in).
- Tree stop: fuse + guard → tmux chord / `claude stop` for background sessions → verify → SIGSTOP → SIGINT/TERM/KILL.
- Notifications. SQLite incident store. Post-mortem TUI + `--md`.
- Replay harness with E1/E3/E4/E5-shaped fixtures.

### Later
- **v1.1 Codex:** session/rollout log adapter (paths **not yet verified**), process-group stop. Post-mortem limited to metadata because of C2.
- v1.2 Quota-velocity rules from statusline `rate_limits` (as ccburn does).
- v1.3 Menu-bar app, global hotkey, Windows (Job Objects).
- v2 **Team tier:** shared policy in the repo, collected post-mortems, workflow awareness.

## 7. Risks & mitigations
| Risk | Mitigation |
|---|---|
| **DIY sync hook already caps counts** (E7). "Why install anything?" | Don't compete on counts. Lead with rate/budget/tree triggers, whole-tree stop with verification (E5), and post-mortems. Docs include a side-by-side replay: "count-cap hook vs FanoutFuse" on E4/E5. Ship the count cap as a free extra in the guard |
| **Vendor fixes it.** Anthropic is already active (cap added then removed, E8; system-prompt AgentTool restriction, E6; depth/concurrency limits, E9) | Stay cross-vendor (Codex next). Velocity, budget and post-mortem are unlikely to be exposed per user. Keep v1 a weekend build. Note that vendor fixes so far are blunt (E6 has 91 reactions against it) |
| **Kill may lose work.** Interrupting discards subagent results (E1), and kill ends the session | Default ladder stops before kill and keeps the main session. Kill is opt-in or needs confirmation. Transcripts are kept, so `--resume` works. Pause (SIGSTOP) is offered as a reversible middle step (needs testing) |
| **Stop is incomplete** (E5) | Verify after every step. Escalate on continued spawns. Pause/kill reaches the whole process group |
| **Undocumented / changing files** (`sessions/{pid}.json`, meta.json, JSONL schema) | File-count signal needs no schema. Adapter layer with schema probes. `doctor` warnings. Fixtures per CLI version |
| **Codex logs unverified.** Paths and spawn events are unconfirmed, and payloads are encrypted (C2) | Codex is v1.1, behind a spike. Its post-mortem is metadata only. Don't market Codex until verified |
| **Willingness to pay unproven.** Users lose quota, not cash. Interest is real but small: issue reactions are low (12–22 on the core runaway issues, 1–2 on #77361/#77414), and the biggest monitor has ~1k★ | Plan as **free / OSS** for individuals. Test a **team tier** (shared policy, aggregated post-mortems) via the "≥ 5 teams ask" metric before building anything paid |
| False trips on legitimate big fan-outs (workflows, ultracode) | Warn before trip. Per-project overrides. `ignore 10m`. Generous defaults |
| Privacy concerns about reading transcripts | Metadata only. No network. `purge` |

## 8. Open questions
- Is `agent-*.jsonl` created at dispatch or at the first write? This sets the detection floor.
- Does the tmux-sent Ctrl+X Ctrl+K register reliably, and does it reach deeper descendants (cf. E5)?
- Does Claude Code survive SIGSTOP/SIGCONT mid-stream?
- Codex: rollout paths, spawn event names, and whether it has a blocking pre-tool hook.
