// The FanoutFuse rule engine: pure state and arithmetic, no Claude Code API.
// register.ts feeds it events and acts on its decisions.

import { RULES, type Config, type Limit, type RuleName } from './config.ts'

/** How a subagent was started. */
export type SpawnPath = 'agent' | 'workflow' | 'fork'

export type SpawnRequest = {
  readonly toolUseId: string
  readonly subagentType: string
  readonly parentAgentId?: string
  readonly fork: boolean
  readonly background: boolean
}

export type Level = 'ok' | 'warn' | 'trip'

export type Verdict = {
  readonly rule: RuleName
  readonly level: Exclude<Level, 'ok'>
  readonly value: number
  readonly threshold: number
}

export type Trip = Verdict & { readonly at: number; readonly reason: string }

export type Metrics = Readonly<Record<RuleName, number>> & { readonly growth_n: number }

/** One allowed spawn. Counted from the moment it is allowed: a workflow raises its agents' spawns concurrently. */
export type Spawn = { at: number; path: SpawnPath; parent?: string; depth: number; agentId?: string; toolUseId: string; running: boolean }

const SEC = 1000
const MIN = 60 * SEC

export function classify(r: SpawnRequest): SpawnPath {
  if (r.subagentType === 'workflow-subagent') return 'workflow'
  if (r.fork) return 'fork'
  return 'agent'
}

export class Engine {
  readonly spawns: Spawn[] = []
  readonly denied: Record<SpawnPath | 'tool', number> = { agent: 0, workflow: 0, fork: 0, tool: 0 }
  /** agentId -> its spawn, for depth of nested spawns and for stopping. */
  readonly agents = new Map<string, Spawn>()
  /** Workflow tool_use_id -> its task id (the TaskStop target). */
  readonly workflows = new Map<string, string>()
  readonly usage: { at: number; tokens: number }[] = []
  quota: { resetsAt: string; points: { at: number; pct: number }[] } = { resetsAt: '', points: [] }
  trip: Trip | undefined
  cooldownUntil = 0
  /** Last warn per rule, so a warning is shown once a minute at most. */
  readonly warned = new Map<RuleName, number>()

  constructor(readonly config: Config) {}

  get tripped(): boolean {
    return this.trip !== undefined
  }

  depthOf(parentAgentId?: string): number {
    if (!parentAgentId) return 1
    return (this.agents.get(parentAgentId)?.depth ?? 1) + 1
  }

  metrics(now: number, extra?: Spawn): Metrics {
    const all = extra ? [...this.spawns, extra] : this.spawns
    let rate = 0
    let burst = 0
    let recent = 0
    let prev = 0
    let depth = 0
    const kids = new Map<string, number>()
    for (const s of all) {
      const d = now - s.at
      if (d >= 0 && d < 60 * SEC) rate++
      if (d >= 0 && d < 10 * SEC) burst++
      if (d >= 0 && d < 30 * SEC) recent++
      else if (d >= 30 * SEC && d < 60 * SEC) prev++
      if (s.path === 'agent' && s.depth > depth) depth = s.depth
      // Breadth only counts Agent-tool children: a workflow fans out by design.
      if (s.path === 'agent') {
        const key = s.parent ?? 'main'
        kids.set(key, (kids.get(key) ?? 0) + 1)
      }
    }
    let running = 0
    for (const s of all) if (s.running) running++
    let tokens = 0
    for (const u of this.usage) if (now - u.at >= 0 && now - u.at < MIN) tokens += u.tokens
    return {
      spawn_rate: rate,
      spawn_burst: burst,
      total_spawns: all.length,
      max_depth: depth,
      running_concurrent: running,
      tokens_per_min: tokens,
      growth_factor: prev > 0 ? recent / prev : 0,
      growth_n: recent,
      breadth_per_parent: Math.max(0, ...kids.values()),
      quota_burn: this.quotaBurn(now),
      quota_used: this.quotaUsed(),
    }
  }

  private limit(rule: RuleName, now: number): Limit {
    const l = this.config.limits[rule]
    const k = now < this.cooldownUntil ? 2 : 1
    return { warn: l.warn * k, trip: l.trip * k }
  }

  /** The worst level each rule is at, trip first. */
  evaluate(now: number, m: Metrics = this.metrics(now)): Verdict[] {
    const out: Verdict[] = []
    for (const rule of RULES) {
      const { warn, trip } = this.limit(rule, now)
      const value = m[rule]
      if (rule === 'growth_factor') {
        if (trip > 0 && value >= trip && m.growth_n >= this.config.growthMinN.trip) out.push({ rule, level: 'trip', value, threshold: trip })
        else if (warn > 0 && value >= warn && m.growth_n >= this.config.growthMinN.warn) out.push({ rule, level: 'warn', value, threshold: warn })
        continue
      }
      if (trip > 0 && value >= trip) out.push({ rule, level: 'trip', value, threshold: trip })
      else if (warn > 0 && value >= warn) out.push({ rule, level: 'warn', value, threshold: warn })
    }
    return out.sort((a, b) => (a.level === b.level ? 0 : a.level === 'trip' ? -1 : 1))
  }

  latch(v: Verdict, now: number): Trip {
    if (!this.trip) {
      this.trip = { ...v, at: now, reason: `FanoutFuse tripped ${v.rule}: ${fmt(v.value)} >= ${fmt(v.threshold)}` }
    }
    return this.trip
  }

  /**
   * Decides on one spawn before it starts. The spawn that would take a rule
   * to its trip value is refused, so the counts of refused spawns are real.
   */
  preSpawn(r: SpawnRequest, now: number): { allow: true; spawn: Spawn; warn?: Verdict } | { allow: false; trip: Trip } {
    const path = classify(r)
    if (this.trip) {
      this.denied[path]++
      return { allow: false, trip: this.trip }
    }
    const candidate: Spawn = { at: now, path, parent: r.parentAgentId, depth: this.depthOf(r.parentAgentId), toolUseId: r.toolUseId, running: true }
    const verdicts = this.evaluate(now, this.metrics(now, candidate))
    const top = verdicts[0]
    if (top?.level === 'trip') {
      this.denied[path]++
      return { allow: false, trip: this.latch(top, now) }
    }
    this.spawns.push(candidate)
    return { allow: true, spawn: candidate, warn: top }
  }

  /** Core started the allowed spawn. */
  started(spawn: Spawn, agentId: string | undefined): void {
    spawn.agentId = agentId
    if (agentId) this.agents.set(agentId, spawn)
    else spawn.running = false
  }

  /** Core refused or failed the allowed spawn: it never ran. */
  cancelled(spawn: Spawn): void {
    const i = this.spawns.indexOf(spawn)
    if (i >= 0) this.spawns.splice(i, 1)
  }

  /** Records a spawn outside preSpawn (tests and replays). */
  record(r: SpawnRequest, agentId: string | undefined, now: number): void {
    const d = { at: now, path: classify(r), parent: r.parentAgentId, depth: this.depthOf(r.parentAgentId), toolUseId: r.toolUseId, running: true }
    this.spawns.push(d)
    this.started(d, agentId)
  }

  finished(agentId: string, tokens: number, now: number): void {
    const a = this.agents.get(agentId)
    if (a) a.running = false
    if (tokens > 0) this.usage.push({ at: now, tokens })
    while (this.usage.length && now - this.usage[0]!.at > 2 * MIN) this.usage.shift()
  }

  /** Ids of agents still running, and of the workflow whose Workflow call started each. */
  running(): { agentId: string; workflowTask?: string; path: SpawnPath }[] {
    const out: { agentId: string; workflowTask?: string; path: SpawnPath }[] = []
    for (const [agentId, a] of this.agents) {
      if (!a.running) continue
      out.push({ agentId, path: a.path, workflowTask: a.path === 'workflow' ? this.workflows.get(a.toolUseId) : undefined })
    }
    return out
  }

  /** Feeds one five_hour reading. Readings jitter by a point, so the series keeps the running max. */
  measureQuota(pct: number, resetsAt: string, now: number): void {
    if (resetsAt !== this.quota.resetsAt) this.quota = { resetsAt, points: [] }
    const pts = this.quota.points
    const last = pts[pts.length - 1]
    pts.push({ at: now, pct: Math.max(pct, last?.pct ?? 0) })
    const keep = this.config.quotaWindowMin * MIN
    while (pts.length > 2 && now - pts[1]!.at >= keep) pts.shift()
  }

  /** Percentage points of the five_hour window burned per minute, over the configured window. */
  quotaBurn(_now: number): number {
    const pts = this.quota.points
    if (pts.length < 2) return 0
    // measureQuota keeps one reading at or before the window start as the baseline.
    const first = pts[0]!
    const last = pts[pts.length - 1]!
    const delta = last.pct - first.pct
    if (delta < this.config.quotaMinDeltaPts || delta <= 0) return 0
    // At least one minute of elapsed time, so two readings a second apart cannot spike the rate.
    const minutes = Math.max(1, (last.at - first.at) / MIN)
    return Math.round((delta / minutes) * 10) / 10
  }

  quotaUsed(): number {
    const pts = this.quota.points
    return pts.length ? pts[pts.length - 1]!.pct : 0
  }

  /** Clears a trip; thresholds stay doubled for the cooldown. */
  resume(now: number): void {
    this.trip = undefined
    this.cooldownUntil = now + this.config.cooldownMs
    this.warned.clear()
  }

  /** True when a warning for this rule has not been shown in the last minute. */
  shouldWarn(rule: RuleName, now: number): boolean {
    const last = this.warned.get(rule)
    if (last !== undefined && now - last < MIN) return false
    this.warned.set(rule, now)
    return true
  }

  counts(): { spawned: Record<SpawnPath, number>; denied: Record<SpawnPath | 'tool', number> } {
    const spawned: Record<SpawnPath, number> = { agent: 0, workflow: 0, fork: 0 }
    for (const s of this.spawns) spawned[s.path]++
    return { spawned, denied: { ...this.denied } }
  }
}

export function fmt(n: number): string {
  return Number.isInteger(n) ? String(n) : n.toFixed(1)
}
