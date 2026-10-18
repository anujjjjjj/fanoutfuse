// FanoutFuse for Claude Code: refuses runaway subagent fan-out at the spawn
// point and stops what is already running.
//
// Every subagent start passes through `agent.spawn`: Agent/Task calls, each
// agent of a Workflow (ultracode runs are workflows), and forks. A settings
// SubagentStart hook cannot do this: Claude Code ignores its exit code 2.

import { parseConfig, type Env, type Options } from '../src/config.ts'
import { Engine, fmt, type SpawnRequest, type Trip, type Verdict } from '../src/engine.ts'
import { renderReport, type Incident, type StopRecord } from '../src/report.ts'
import type {
  AgentSpawnInput,
  AgentSpawnResult,
  Api,
  CommandRunInput,
  CommandRunResult,
  On,
  SessionMeasureInput,
  ToolCallInput,
  ToolCallResult,
  TurnCompleteInput,
  TurnStartInput,
} from '../src/api.ts'

const SPAWN_TOOLS = new Set(['Agent', 'Task', 'Workflow'])

/**
 * Reads every variable the config knows. Each name is a literal so the mods
 * loader can list what the module reads; tests/config.test.ts checks this
 * list against parseConfig.
 */
async function readEnv($: Api): Promise<Env> {
  return {
    HOME: await $.env.get('HOME'),
    FANOUTFUSE_HOME: await $.env.get('FANOUTFUSE_HOME'),
    FANOUTFUSE_QUOTA_WINDOW_MIN: await $.env.get('FANOUTFUSE_QUOTA_WINDOW_MIN'),
    FANOUTFUSE_QUOTA_MIN_DELTA_PTS: await $.env.get('FANOUTFUSE_QUOTA_MIN_DELTA_PTS'),
    FANOUTFUSE_STOP_RUNNING: await $.env.get('FANOUTFUSE_STOP_RUNNING'),
    FANOUTFUSE_ABORT_TURN: await $.env.get('FANOUTFUSE_ABORT_TURN'),
    FANOUTFUSE_VERIFY_AFTER_MS: await $.env.get('FANOUTFUSE_VERIFY_AFTER_MS'),
    FANOUTFUSE_SPAWN_RATE_WARN: await $.env.get('FANOUTFUSE_SPAWN_RATE_WARN'),
    FANOUTFUSE_SPAWN_RATE_TRIP: await $.env.get('FANOUTFUSE_SPAWN_RATE_TRIP'),
    FANOUTFUSE_SPAWN_BURST_WARN: await $.env.get('FANOUTFUSE_SPAWN_BURST_WARN'),
    FANOUTFUSE_SPAWN_BURST_TRIP: await $.env.get('FANOUTFUSE_SPAWN_BURST_TRIP'),
    FANOUTFUSE_TOTAL_SPAWNS_WARN: await $.env.get('FANOUTFUSE_TOTAL_SPAWNS_WARN'),
    FANOUTFUSE_TOTAL_SPAWNS_TRIP: await $.env.get('FANOUTFUSE_TOTAL_SPAWNS_TRIP'),
    FANOUTFUSE_MAX_DEPTH_WARN: await $.env.get('FANOUTFUSE_MAX_DEPTH_WARN'),
    FANOUTFUSE_MAX_DEPTH_TRIP: await $.env.get('FANOUTFUSE_MAX_DEPTH_TRIP'),
    FANOUTFUSE_RUNNING_CONCURRENT_WARN: await $.env.get('FANOUTFUSE_RUNNING_CONCURRENT_WARN'),
    FANOUTFUSE_RUNNING_CONCURRENT_TRIP: await $.env.get('FANOUTFUSE_RUNNING_CONCURRENT_TRIP'),
    FANOUTFUSE_TOKENS_PER_MIN_WARN: await $.env.get('FANOUTFUSE_TOKENS_PER_MIN_WARN'),
    FANOUTFUSE_TOKENS_PER_MIN_TRIP: await $.env.get('FANOUTFUSE_TOKENS_PER_MIN_TRIP'),
    FANOUTFUSE_GROWTH_FACTOR_WARN: await $.env.get('FANOUTFUSE_GROWTH_FACTOR_WARN'),
    FANOUTFUSE_GROWTH_FACTOR_TRIP: await $.env.get('FANOUTFUSE_GROWTH_FACTOR_TRIP'),
    FANOUTFUSE_BREADTH_PER_PARENT_WARN: await $.env.get('FANOUTFUSE_BREADTH_PER_PARENT_WARN'),
    FANOUTFUSE_BREADTH_PER_PARENT_TRIP: await $.env.get('FANOUTFUSE_BREADTH_PER_PARENT_TRIP'),
    FANOUTFUSE_QUOTA_BURN_WARN: await $.env.get('FANOUTFUSE_QUOTA_BURN_WARN'),
    FANOUTFUSE_QUOTA_BURN_TRIP: await $.env.get('FANOUTFUSE_QUOTA_BURN_TRIP'),
    FANOUTFUSE_QUOTA_USED_WARN: await $.env.get('FANOUTFUSE_QUOTA_USED_WARN'),
    FANOUTFUSE_QUOTA_USED_TRIP: await $.env.get('FANOUTFUSE_QUOTA_USED_TRIP'),
  }
}

type State = {
  options: Options
  engine: Engine | undefined
  configErrors: readonly string[]
  sessionId: string
  mainTurn: string | undefined
  commandReady: boolean
  incident: Incident | undefined
}

async function ensure(st: State, $: Api): Promise<Engine> {
  if (st.engine) return st.engine
  const parsed = parseConfig(st.options, await readEnv($))
  st.configErrors = parsed.errors
  st.engine = new Engine(parsed.config)
  st.sessionId = (await $.session.id().catch(() => '')) || 'unknown'
  for (const err of st.configErrors) $.ui.log(`fanoutfuse config: ${err}`)
  if (!st.commandReady) {
    st.commandReady = true
    await $.command
      .register({ name: 'fanoutfuse', description: 'FanoutFuse: status, report, or resume after a trip', argumentHint: '[status|report|resume]', immediate: true })
      .catch(() => undefined)
  }
  return st.engine
}

/** The Go CLI's manual fuse (`fanoutfuse stop`) also holds plugin sessions. */
async function fuseFile(st: State, $: Api, eng: Engine): Promise<string | undefined> {
  const home = eng.config.home
  if (!home) return undefined
  for (const name of [st.sessionId, 'ALL']) {
    if (name && (await $.fs.exists(`${home}/fuse/${name}`).catch(() => false))) return `${home}/fuse/${name}`
  }
  return undefined
}

function denyText(trip: Trip): string {
  return `${trip.reason}. FanoutFuse refused this spawn to stop runaway subagent fan-out. Do not retry spawning; finish with the agents you have, or ask the user to run /fanoutfuse resume.`
}

function warn($: Api, eng: Engine, v: Verdict | undefined, now: number): void {
  if (!v || v.level !== 'warn' || !eng.shouldWarn(v.rule, now)) return
  $.ui.toast(`FanoutFuse warning: ${v.rule} ${fmt(v.value)} (trips at ${fmt(eng.config.limits[v.rule].trip)})`, { timeoutMs: 8000 })
}

async function writeIncident(st: State, $: Api, eng: Engine): Promise<void> {
  if (!st.incident || !eng.config.home) return
  const at = new Date(st.incident.trip.at).toISOString().replace(/[:.]/g, '-')
  await $.fs.write(`${eng.config.home}/incidents/plugin-${st.sessionId}-${at}.json`, JSON.stringify(st.incident, null, 2) + '\n').catch(() => undefined)
}

async function stopTargets($: Api, eng: Engine): Promise<string[]> {
  const ids = new Set<string>()
  const listed = await $.agent.list().catch(() => [])
  for (const a of listed) if (a.status === 'running') ids.add(a.id)
  for (const r of eng.running()) ids.add(r.workflowTask ?? r.agentId)
  return [...ids]
}

async function stopAll($: Api, eng: Engine, attempt: number): Promise<StopRecord[]> {
  const out: StopRecord[] = []
  for (const id of await stopTargets($, eng)) {
    const r = await $.tool.call({ tool: 'TaskStop', task_id: id }).catch((err: unknown) => ({ deny: String(err) }) as ToolCallResult)
    const ok = !r.deny && !r.isError
    out.push({ id, attempt, ok, detail: (r.deny ?? r.text ?? '').slice(0, 300) })
    if (ok) {
      // A stopped workflow takes its agents with it.
      const now = await $.clock.now()
      for (const run of eng.running()) if (run.agentId === id || run.workflowTask === id) eng.finished(run.agentId, 0, now)
    }
  }
  return out
}

async function onTrip(st: State, $: Api, eng: Engine, trip: Trip): Promise<void> {
  st.incident = {
    session: st.sessionId,
    trip,
    counts: eng.counts(),
    metrics: eng.metrics(trip.at),
    stops: [],
    stopVerified: null,
    abortedTurn: null,
  }
  $.ui.toast(`${trip.reason}. New subagents are refused${eng.config.stopRunning ? '; stopping running agents' : ''}. /fanoutfuse report for details.`, { timeoutMs: 15000 })
  $.ui.log(`fanoutfuse: ${trip.reason}`)
  await writeIncident(st, $, eng)
  if (eng.config.stopRunning) {
    st.incident.stops.push(...(await stopAll($, eng, 1)))
    $.clock.after(eng.config.verifyAfterMs, () => void verify(st, $, eng))
  }
  if (eng.config.abortTurn && st.mainTurn) {
    st.incident.abortedTurn = st.mainTurn
    await $.turn.abort({ turnId: st.mainTurn }).catch(() => undefined)
  }
  st.incident.counts = eng.counts()
  await writeIncident(st, $, eng)
}

/** Re-checks after a stop; anything still running is stopped again and reported. */
async function verify(st: State, $: Api, eng: Engine): Promise<void> {
  const inc = st.incident
  if (!inc) return
  const left = await stopTargets($, eng)
  if (left.length === 0) {
    inc.stopVerified = true
  } else {
    inc.stops.push(...(await stopAll($, eng, 2)))
    inc.stopVerified = (await stopTargets($, eng)).length === 0
    if (!inc.stopVerified) $.ui.toast(`FanoutFuse: ${left.length} agent(s) still running after two stops: ${left.join(', ')}`, { timeoutMs: 15000 })
  }
  inc.counts = eng.counts()
  await writeIncident(st, $, eng)
}

/** Keeps the incident file's refused counts current after the trip. */
async function recount(st: State, $: Api, eng: Engine): Promise<void> {
  if (!st.incident) return
  st.incident.counts = eng.counts()
  await writeIncident(st, $, eng)
}

/** Checks rules that move between spawns (quota, tokens) and trips on them. */
async function check(st: State, $: Api, eng: Engine): Promise<void> {
  if (eng.tripped) return
  const now = await $.clock.now()
  const top = eng.evaluate(now)[0]
  if (top?.level === 'trip') await onTrip(st, $, eng, eng.latch(top, now))
  else warn($, eng, top, now)
}

export function register(on: On, options: Options) {
  const st: State = { options, engine: undefined, configErrors: [], sessionId: '', mainTurn: undefined, commandReady: false, incident: undefined }

  on('session.start', async ($: Api, e: unknown, next: (e: unknown) => Promise<unknown>) => {
    await ensure(st, $)
    return next(e)
  })

  // A headless run can end before the verify timer fires: check once more on the way out.
  on('session.end', async ($: Api, e: unknown, next: (e: unknown) => Promise<unknown>) => {
    const eng = st.engine
    if (eng && st.incident && st.incident.stopVerified === null && eng.config.stopRunning) await verify(st, $, eng)
    return next(e)
  })

  on('turn.start', async ($: Api, e: TurnStartInput, next: (e: TurnStartInput) => Promise<unknown>) => {
    st.mainTurn = e.turnId
    return next(e)
  })

  // Every subagent start: Agent/Task, each Workflow (and ultracode) agent, forks.
  on('agent.spawn', async ($: Api, e: AgentSpawnInput, next: (e: AgentSpawnInput) => Promise<AgentSpawnResult>) => {
    const eng = await ensure(st, $)
    const req: SpawnRequest = { toolUseId: e.tool_use_id, subagentType: e.subagentType, parentAgentId: e.parentAgentId, fork: e.fork, background: e.background }
    const fuse = await fuseFile(st, $, eng)
    if (fuse) {
      eng.denied[req.fork ? 'fork' : req.subagentType === 'workflow-subagent' ? 'workflow' : 'agent']++
      return { deny: `FanoutFuse fuse is set (${fuse}); remove it with \`fanoutfuse resume\`.` }
    }
    const now = await $.clock.now()
    const wasTripped = eng.tripped
    const d = eng.preSpawn(req, now)
    if (!d.allow) {
      if (!wasTripped) $.clock.after(0, () => void onTrip(st, $, eng, d.trip))
      else await recount(st, $, eng)
      return { deny: denyText(d.trip) }
    }
    warn($, eng, d.warn, now)
    let r: AgentSpawnResult
    try {
      r = await next(e)
    } catch (err) {
      eng.cancelled(d.spawn)
      throw err
    }
    if (r.deny) eng.cancelled(d.spawn)
    else eng.started(d.spawn, r.agentId)
    return r
  }).catch(async ($: Api, e: AgentSpawnInput, next: ((e: AgentSpawnInput) => Promise<AgentSpawnResult>) & { called?: boolean }) => {
    // Fail open on an internal error, unless the fuse already tripped.
    if (next.called) return next(e)
    if (st.engine?.trip) return { deny: denyText(st.engine.trip) }
    return next(e)
  })

  // The tools that start subagents are refused outright while tripped, and
  // a Workflow's task id is kept as the TaskStop target for its agents.
  on('tool.call', async ($: Api, e: ToolCallInput, next: (e: ToolCallInput) => Promise<ToolCallResult>) => {
    if (!SPAWN_TOOLS.has(e.tool)) return next(e)
    const eng = await ensure(st, $)
    const fuse = await fuseFile(st, $, eng)
    if (fuse) {
      eng.denied.tool++
      return { deny: `FanoutFuse fuse is set (${fuse}); remove it with \`fanoutfuse resume\`.` }
    }
    if (eng.trip) {
      eng.denied.tool++
      await recount(st, $, eng)
      return { deny: denyText(eng.trip) }
    }
    const r = await next(e)
    if (e.tool === 'Workflow' && r.result && typeof r.result === 'object') {
      const taskId = (r.result as { taskId?: unknown }).taskId
      if (typeof taskId === 'string') eng.workflows.set(e.tool_use_id, taskId)
    }
    return r
  }).catch(async ($: Api, e: ToolCallInput, next: ((e: ToolCallInput) => Promise<ToolCallResult>) & { called?: boolean }) => {
    if (next.called) return next(e)
    if (st.engine?.trip && SPAWN_TOOLS.has(e.tool)) return { deny: denyText(st.engine.trip) }
    return next(e)
  })

  on('turn.complete', async ($: Api, e: TurnCompleteInput, next: (e: TurnCompleteInput) => Promise<unknown>) => {
    const r = await next(e)
    const eng = await ensure(st, $)
    if (e.agentId) {
      const u = e.usage
      const tokens = u ? u.input_tokens + u.output_tokens + u.cache_read_input_tokens + u.cache_creation_input_tokens : 0
      eng.finished(e.agentId, tokens, await $.clock.now())
      await check(st, $, eng)
    } else if (e.turnId === st.mainTurn) {
      st.mainTurn = undefined
    }
    return r
  })

  // The five_hour quota window feeds quota_burn and quota_used.
  on('session.measure', async ($: Api, e: SessionMeasureInput, next: (e: SessionMeasureInput) => Promise<unknown>) => {
    const r = await next(e)
    const five = e.rateLimits.find((l) => l.kind === 'five_hour')
    if (five) {
      const eng = await ensure(st, $)
      eng.measureQuota(five.percentUsed, five.resetsAt ?? '', await $.clock.now())
      await check(st, $, eng)
    }
    return r
  })

  on('command.run', async ($: Api, e: CommandRunInput, next: (e: CommandRunInput) => Promise<CommandRunResult>): Promise<CommandRunResult> => {
    if (e.command !== 'fanoutfuse') return next(e)
    const eng = await ensure(st, $)
    const now = await $.clock.now()
    const arg = e.args.trim().split(/\s+/)[0] || 'status'
    if (arg === 'resume') {
      if (!eng.trip) return { text: 'FanoutFuse is not tripped.' }
      const was = eng.trip.reason
      eng.resume(now)
      st.incident = undefined
      return { text: `FanoutFuse resumed (${was}). Thresholds are doubled for ${Math.round(eng.config.cooldownMs / 60000)} minutes.` }
    }
    if (arg === 'report') return { text: renderReport({ engine: eng, incident: st.incident, now, configErrors: st.configErrors }) }
    if (arg === 'status') {
      const m = eng.metrics(now)
      const c = eng.counts()
      const state = eng.trip ? `TRIPPED: ${eng.trip.reason}` : 'armed'
      return {
        text: `FanoutFuse ${state}. spawned agent=${c.spawned.agent} workflow=${c.spawned.workflow} fork=${c.spawned.fork}; refused ${c.denied.agent + c.denied.workflow + c.denied.fork} spawns, ${c.denied.tool} tool calls; running ${m.running_concurrent}; five_hour ${fmt(m.quota_used)}% at ${fmt(m.quota_burn)} pts/min.`,
      }
    }
    return { text: 'Usage: /fanoutfuse [status|report|resume]' }
  }).catch(async ($: Api, e: CommandRunInput, next: (e: CommandRunInput) => Promise<CommandRunResult>) => {
    if (e.command === 'fanoutfuse') return { text: 'FanoutFuse: the command failed; the guard itself is unaffected.' }
    return next(e)
  })
}
