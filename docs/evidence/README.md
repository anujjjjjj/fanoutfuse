# Evidence

Recorded runs of real Claude Code sessions on 2026-10-18 (Claude Code 2.1.300,
Linux, signed in on a subscription). Each run directory holds:

| File | What it is |
| :- | :- |
| `stream.jsonl` | `claude -p --output-format stream-json --verbose` output. The init line is trimmed: the recording account's MCP servers, other plugins and slash commands are removed. |
| `fanoutfuse-home/incidents/*.json` | The incident FanoutFuse wrote: rule, counts started and refused per spawn path, each TaskStop and whether the stop was verified. |
| `workflow-journal.jsonl` | Claude Code's own journal of the workflow run: `started` per agent that ran, `failed` per agent the plugin refused. |
| `summary.txt` | Output of `scripts/proof/summarize.sh`. |

## Runs

| Run | Setup | Started | Refused | Stopped | Stop verified |
| :- | :- | -: | -: | :- | :- |
| `2026-10-18/agent` | Sonnet, three parallel background Agent calls, `total_spawns` trip 2 | 1 | 2 | the running agent (TaskStop ok) | yes |
| `2026-10-18/workflow` | Sonnet, one Workflow with five agents, trip 3 | 2 | 3 | the workflow task (TaskStop ok) | yes |
| `2026-10-18/ultracode` | Opus, `--effort ultracode` (transcript shows the "Ultracode is on" reminder and `effort: xhigh`), the prompt asked for a three-agent workflow and Claude wrote the script, trip 2 | 1 | 2 | the workflow task (TaskStop ok) | yes |
| `2026-10-18/subagentstart-exit2` | A settings `SubagentStart` hook that always exits 2, one Agent call and a two-agent Workflow, no FanoutFuse | 3 | 0 | n/a | n/a |

The last run is why FanoutFuse does not rely on `SubagentStart`: the hook fired
three times, exited 2 each time, and all three subagents still answered
(`PONG-7731`, `ALPHA-1`, `BETA-2`). That matches the hooks reference
(https://code.claude.com/docs/en/hooks, "Exit code 2 behavior per event":
SubagentStart, "Can block? No").

Reproduce with `sh scripts/proof/prove.sh` and `sh scripts/proof/subagentstart-exit2.sh`.
