# FanoutFuse

FanoutFuse is a local circuit breaker for runaway subagent sessions. It watches transcript file creation and metadata, detects excessive spawn rate, bursts, growth, breadth, depth, concurrency, and optional token velocity, then blocks new spawns and stops background work. SQLite incident reports explain what tripped and whether spawning continued.

It runs on Linux and macOS as one pure-Go binary. There is no telemetry or network service. Project transcripts stay read-only. Prompts and response text are never stored.

![Replay of fixture 69332 followed by a guard deny](docs/media/replay-69332.gif)

## Install

```sh
brew install anujjjjjj/tap/fanoutfuse
```

Or download a binary from the [v0.1.0 release](https://github.com/anujjjjjj/fanoutfuse/releases/tag/v0.1.0).

## Build and start

Requires Go 1.24 and, for graceful background stopping, tmux.

```sh
CGO_ENABLED=0 go build -o fanoutfuse ./cmd/fanoutfuse
./fanoutfuse install
./fanoutfuse doctor
./fanoutfuse run
```

Keep `run` active in a terminal or under your user service manager. Install adds a synchronous `PreToolUse` hook for `Agent` and `Task`, using the absolute binary path. Existing settings are preserved; the original settings receive a `.fanoutfuse.bak` backup. Repeating install or uninstall is safe. Keep the binary at its installed path, or run install again after moving it.

## Commands

```sh
fanoutfuse status
fanoutfuse stop                    # contain all live sessions
fanoutfuse pause <session-or-pid>   # SIGSTOP to the process group
fanoutfuse resume <session-or-pid>  # SIGCONT, clear containment, temporary doubled limits
fanoutfuse pm                      # latest incident, plain text
fanoutfuse pm --md [incident-id]    # metadata-only Markdown export
fanoutfuse doctor
fanoutfuse config --explain
fanoutfuse uninstall
fanoutfuse purge                   # stop the daemon process first
```

The default ladder writes a fuse and sends `C-x C-k` twice to the session's tmux pane. Each verification window lasts 20 seconds. Continued spawning is recorded. Process-group pause and kill require explicit configuration. Without tmux, the skipped stop is recorded; the fuse still blocks new spawn calls. Pause and resume also work when the daemon is down. Signals refuse to target FanoutFuse's own process group.

`guard` is a daemon-independent hook command. It reads hook JSON from stdin and checks session and global fuse files, allowing calls silently or returning a deny decision with the trip reason.

## Replay

```sh
fanoutfuse replay fixtures/69332.yaml
fanoutfuse replay fixtures/68619-burst.yaml
fanoutfuse replay fixtures/77361-breadth.yaml
fanoutfuse replay fixtures/77414-post-stop.yaml
fanoutfuse replay fixtures/normal-pr-review.yaml
fanoutfuse replay fixtures/planned-20.yaml
fanoutfuse pm --md
```

Replays use virtual time, complete in seconds, and write real incidents and fuses. Signals and tmux operations are simulated. Use an isolated state directory for experiments:

```sh
export FANOUTFUSE_HOME="$(mktemp -d)/state"
fanoutfuse replay fixtures/77414-post-stop.yaml
fanoutfuse pm --md
```

| Fixture | Expected detection | Plain 50-count cap |
| --- | --- | --- |
| 69332 | Burst at agent 25 | Agent 50 |
| 68619-burst | Burst at agent 15 | Agent 50 |
| 77361-breadth | Breadth at agent 10 | Agent 50 |
| 77414-post-stop | Rate at agent 30; simulated pause after continued spawns | Agent 50; no stop verification |
| normal-pr-review | No trip, 5 agents | No trip |
| planned-20 | No trip, 20 over 3 minutes | No trip |

The 69332 fixture retains minute totals of 2, 8, 17, 8, 16, and 188. Its explicit eight-second dispatch window models clustered launches; minute totals alone cannot specify a burst. Fixtures include parent attribution, subagent types, and input/output/cache token usage. The breadth fixture reaches depth five; the other fixtures span depths one through three.

## Configuration

Settings merge in this order: defaults, `$HOME/.config/fanoutfuse/config.toml`, nearest ancestor `.fanoutfuse.toml` of the session directory, then environment. `$XDG_CONFIG_HOME` overrides the user config base.

```toml
[general]
action_ladder = ["warn", "contain", "stop_background"]
escalate_after = "20s"
notify = true
bell = true

[limits]
spawn_rate_per_min = { warn = 15, trip = 30 }
spawn_burst_per_10s = { warn = 8, trip = 15 }
total_spawns = { warn = 40, trip = 80 }
max_depth = { warn = 3, trip = 4 }
running_concurrent = { warn = 15, trip = 25 }
breadth_per_parent = { warn = 6, trip = 10 }
growth_factor_30s = { warn = 3, trip = 4, min_n_warn = 10, min_n_trip = 20 }
tokens_per_min = { warn = 0, trip = 0 }

[cooldown]
after_resume = "10m"
```

Add `"pause"` after `"stop_background"` to enable automatic process-group suspension when spawns continue. Add `"kill"` after pause only if you want automatic `SIGINT`, followed by `SIGTERM` after three seconds and `SIGKILL` after another five seconds. Resuming temporarily doubles limits for the configured cooldown.

`FANOUTFUSE_HOME` defaults to `$HOME/.fanoutfuse`. Limit overrides use `FANOUTFUSE_<RULE>_WARN` and `FANOUTFUSE_<RULE>_TRIP`, for example `FANOUTFUSE_TOTAL_SPAWNS_TRIP=1`. `FANOUTFUSE_ESCALATE_AFTER` and comma-separated `FANOUTFUSE_ACTION_LADDER` override the ladder. A zero threshold disables that rule level.

Markdown exports use GitHub tables for key facts, depth counts, top-five branches, and actions. Branches rank by tokens, then direct children. Repeated verification windows share one entry per step; each action records spawns observed while it was the latest step.

Post-mortems include ten-second spawn buckets, depth counts, the five loudest branches, action timestamps, spawns after trip, and a five-minute rate projection. Token prices are illustrative API-equivalent estimates, not subscription billing. `purge` removes the private state directory, including incidents and fuses; uninstall the hook separately if retiring the installation.

## Deploy

The site is static in `/site`. Deploy it on Vercel with the root directory set to `site` (`cd site && vercel --prod`). Vercel is the canonical host; GitHub Pages is not used.
