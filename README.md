# FanoutFuse

A fuse for runaway subagent fan-out in Claude Code. When a session starts too
many subagents, too fast, too deep, or burns the 5-hour usage window too
quickly, FanoutFuse refuses the next spawn on every path (Agent/Task,
Workflow, ultracode) and stops the agents and workflows already running.

It is a Claude Code plugin. No daemon, no tmux, no network calls, no telemetry.

## Install

```sh
claude plugin install fanoutfuse --marketplace anujjjjjj/fanoutfuse
```

That one command needs Claude Code 2.1.292 or later. Inside a session (2.1.275 or later):

```text
/plugin install fanoutfuse --marketplace anujjjjjj/fanoutfuse
```

Tested on Claude Code 2.1.300. Change limits with `/plugin configure fanoutfuse`
or `/config`, or with environment variables (see [Configuration](#configuration)).

## What it catches

Claude Code raises the plugin's `agent.spawn` hook before each subagent starts:
Agent/Task calls, each agent of a workflow, and forks. The tools that start
subagents also pass through its `tool.call` hook.

| Spawn path | Refused at | Unit tests | Real run |
| :- | :- | :- | :- |
| Agent tool (and its `Task` alias) | `agent.spawn`; `Agent`/`Task` calls while tripped | `plugin/tests/spawn-paths.test.ts` | [agent](docs/evidence/2026-10-18/agent) |
| Workflow agents | `agent.spawn` (each agent, as `workflow-subagent`); `Workflow` calls while tripped | `plugin/tests/spawn-paths.test.ts` | [workflow](docs/evidence/2026-10-18/workflow) |
| Ultracode | Same as Workflow: ultracode runs its work as workflows | Workflow tests | [ultracode](docs/evidence/2026-10-18/ultracode) |
| Forks | `agent.spawn` (`fork: true`) | `plugin/tests/spawn-paths.test.ts` | not recorded |
| Nested spawns | `agent.spawn`, depth from `parentAgentId` | `plugin/tests/spawn-paths.test.ts` | not recorded |

Workflow agents never reach `PreToolUse:Agent`, so a `PreToolUse` hook matched
on `Agent|Task` misses them. FanoutFuse gates the spawn itself.

### Why not a SubagentStart hook

A settings `SubagentStart` hook that exits 2 does not stop the subagent. The
[hooks reference](https://code.claude.com/docs/en/hooks) ("Exit code 2 behavior
per event") lists SubagentStart as "Can block? No". We checked it on Claude Code
2.1.300: the hook fired three times and exited 2 each time, and all three
subagents (one Agent call, two Workflow agents) still answered
([evidence](docs/evidence/2026-10-18/subagentstart-exit2),
[script](scripts/proof/subagentstart-exit2.sh)).

## What happens on a trip

1. **Refuse.** The spawn that would take a rule to its trip value is refused, and so is every spawn after it. `Agent`, `Task` and `Workflow` calls are refused too. The model gets the reason as the tool error.
2. **Stop.** Unless `stop_running` is off, FanoutFuse calls `TaskStop` on every running agent `$.agent.list()` reports, and on every workflow task this session launched. Workflow agents don't show up in that list, so they are stopped through their workflow.
3. **Verify.** 5 seconds later, and again when the session ends, it checks what is still running. Anything left gets one more `TaskStop`. If something survives both, you get a toast and the incident says `stopVerified: false`.
4. **Optional:** with `abort_turn` on, it also interrupts the main conversation's current turn.

Each trip writes `~/.fanoutfuse/incidents/plugin-<session>-<time>.json` (or under
`$FANOUTFUSE_HOME`). It holds the rule, spawns started and refused per path,
every stop and whether it held.

```text
/fanoutfuse status   # armed or tripped, counts, running agents, 5-hour burn
/fanoutfuse report   # markdown: per-path table, every rule against its limits, stops
/fanoutfuse resume   # clear a trip; limits are doubled for 10 minutes
```

## Rules

A rule trips when its value reaches the trip number. 0 turns a level off.

| Rule | Measures | Warn | Trip |
| :- | :- | -: | -: |
| `spawn_rate` | spawns in the last 60 s | 20 | 40 |
| `spawn_burst` | spawns in the last 10 s | 12 | 24 |
| `total_spawns` | spawns this session | 60 | 120 |
| `max_depth` | nesting depth of Agent-tool subagents | 3 | 4 |
| `running_concurrent` | subagents running now | 20 | 32 |
| `tokens_per_min` | subagent tokens in the last 60 s | off | off |
| `growth_factor` | spawns in the last 30 s ÷ the 30 s before, once 10 (warn) or 20 (trip) landed | 3 | 4 |
| `breadth_per_parent` | Agent-tool children of one parent (workflows are exempt) | 6 | 10 |
| `quota_burn` | 5-hour window, percentage points per minute | 2 | 5 |
| `quota_used` | 5-hour window, percent used | off | off |

A workflow runs up to 16 agents at once, so the burst and concurrency defaults
leave room for one full workflow.

`quota_burn` reads the 5-hour window Claude Code reports after each response.
It measures over the last 5 minutes, counts at least one minute of elapsed time,
and ignores moves under 3 points. Readings jitter by a point, so it uses the
highest reading so far. A new window (a new reset time) starts over. With an API
key instead of a subscription there is no 5-hour window, and both quota rules
stay idle.

Counts are per session. A new session starts at zero.

## Configuration

Order, lowest to highest: built-in defaults, plugin options (`/plugin configure
fanoutfuse` or `/config`), environment variables.

| Setting | Plugin option | Environment | Default |
| :- | :- | :- | :- |
| Any rule's limits | `<rule>_trip` for the rules in the dialog | `FANOUTFUSE_<RULE>_WARN`, `FANOUTFUSE_<RULE>_TRIP` | table above |
| Quota burn window | `quota_window_min` | `FANOUTFUSE_QUOTA_WINDOW_MIN` | 5 |
| Smallest quota move counted | | `FANOUTFUSE_QUOTA_MIN_DELTA_PTS` | 3 |
| Stop running agents on trip | `stop_running` | `FANOUTFUSE_STOP_RUNNING` | true |
| Abort the current turn on trip | `abort_turn` | `FANOUTFUSE_ABORT_TURN` | false |
| Re-check delay after a stop (ms) | | `FANOUTFUSE_VERIFY_AFTER_MS` | 5000 |
| State directory | | `FANOUTFUSE_HOME` | `~/.fanoutfuse` |

The dialog offers the trip values of `total_spawns`, `spawn_rate`, `spawn_burst`,
`running_concurrent`, `max_depth`, `quota_burn` and `quota_used`, plus the warn
value of `quota_burn`. Every rule takes environment variables. A value that
doesn't parse is logged and the default is used, so a typo can't switch the
guard off.

## Prove it on your machine

```sh
sh scripts/proof/prove.sh                    # Agent, Workflow and ultracode runs
sh scripts/proof/subagentstart-exit2.sh      # the SubagentStart exit 2 check
```

Each run uses a throwaway project and a low `total_spawns` trip, loads the
plugin with `--plugin-dir`, and saves the stream, the incident and a summary
under `docs/evidence/<time>/`. It needs a signed-in `claude`. Workflows are
turned on per run with `--settings '{"enableWorkflows":true}'`, so your settings
don't change. The ultracode run uses Opus at xhigh effort and spends a few
percent of the 5-hour window.

## Develop

```sh
cd plugin
claude plugin validate --strict .
claude plugin test .         # every spawn path, the stop ladder, quota burn, config
npm ci && npm run typecheck  # tsc --strict
```

`plugin/hooks/register.ts` is the hooks module. `plugin/src/engine.ts` holds the
rules and counters, `plugin/src/config.ts` the typed config. CI runs the plugin
checks next to the Go jobs.

## Optional: the Go CLI

The original FanoutFuse is a Go binary that watches transcript files from
outside Claude Code. It also offers replays, SQLite post-mortems and a tmux-based
stop. The plugin doesn't need it. The plugin does respect the CLI's manual fuse:
`fanoutfuse stop` refuses spawns in plugin sessions too.

```sh
brew install anujjjjjj/tap/fanoutfuse   # v0.1.0
# or build from source (Go 1.24):
CGO_ENABLED=0 go build -o fanoutfuse ./cmd/fanoutfuse
```

| Command | What it does |
| :- | :- |
| `fanoutfuse install` | Adds a synchronous `PreToolUse` guard for `Agent`, `Task` and `Workflow` to `~/.claude/settings.json` (backup `.fanoutfuse.bak`). Earlier builds matched only `Agent` and `Task`; `doctor` reports that hook as needing a reinstall. |
| `fanoutfuse run` | Watches sessions and trips the fuse. Keep it running in a terminal or under your service manager. It counts workflow agents from `subagents/workflows/<run>/`. |
| `fanoutfuse guard` | The hook command: reads hook JSON on stdin and denies if a session or global fuse is set. |
| `fanoutfuse status`, `stop`, `pause <session>`, `resume <session>` | Show sessions; set a global fuse; SIGSTOP or SIGCONT a session's process group (never FanoutFuse's own). |
| `fanoutfuse pm [--md] [incident]` | Post-mortem of the latest or a given incident. |
| `fanoutfuse replay <fixture.yaml>` | Replays a recorded spawn pattern in virtual time. |
| `fanoutfuse doctor`, `config --explain`, `uninstall`, `purge` | Check the setup, show where each setting came from, remove the hook, delete state. |

The v0.1.0 release and its Homebrew formula predate the `Workflow` matcher.
Build from source for it until the next release.

The CLI's stop ladder is `warn`, `contain` (write the fuse), `stop_background`
(send `C-x C-k` twice to the session's tmux pane), then optional `pause` and
`kill`. Without tmux, `stop_background` is skipped and recorded; the fuse still
refuses new spawns. CLI settings live in `~/.config/fanoutfuse/config.toml` and
`.fanoutfuse.toml`, with the limits above at their older defaults (`fanoutfuse
config --explain` prints them).

### Replays

```sh
export FANOUTFUSE_HOME="$(mktemp -d)/state"
fanoutfuse replay fixtures/69332.yaml
fanoutfuse pm --md
```

| Fixture | Detection (CLI defaults) |
| :- | :- |
| 69332 | spawn_burst at agent 25 |
| 68619-burst | spawn_burst at agent 15 |
| 77361-breadth | breadth_per_parent at agent 10 |
| 77414-post-stop | spawn_rate at agent 30, then a simulated pause after spawning continued |
| normal-pr-review | no trip (5 agents) |
| planned-20 | no trip (20 over 3 minutes) |

A replay simulates signals and tmux; it does not block anything. Its
`spawns_after_trip` is how many spawns the recording had after the trip point.
For 69332 that is 214: spawns that happened in the original session, not spawns
FanoutFuse prevented.

## License

MIT. See [LICENSE](LICENSE).

## Site

The static site in `site/` deploys on Vercel with `site` as the root directory
(`cd site && vercel --prod`).
