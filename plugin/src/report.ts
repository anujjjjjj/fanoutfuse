// The incident record and its markdown report (/fanoutfuse report).

import { RULES } from './config.ts'
import { fmt, type Engine, type Metrics, type SpawnPath, type Trip } from './engine.ts'

export type StopRecord = { id: string; attempt: number; ok: boolean; detail: string }

export type Incident = {
  session: string
  trip: Trip
  counts: { spawned: Record<SpawnPath, number>; denied: Record<SpawnPath | 'tool', number> }
  metrics: Metrics
  stops: StopRecord[]
  /** true: nothing was running at the re-check; false: something survived two stops; null: not checked yet or stopping is off. */
  stopVerified: boolean | null
  abortedTurn: string | null
}

export function renderReport(a: { engine: Engine; incident: Incident | undefined; now: number; configErrors: readonly string[] }): string {
  const { engine, incident, now } = a
  const m = engine.metrics(now)
  const c = engine.counts()
  const lines: string[] = ['## FanoutFuse report', '']
  lines.push(engine.trip ? `**Tripped** at ${new Date(engine.trip.at).toISOString()}: ${engine.trip.reason}` : '**Armed**, not tripped.')
  lines.push('', '| path | started | refused |', '| :- | -: | -: |')
  for (const p of ['agent', 'workflow', 'fork'] as const) lines.push(`| ${p} | ${c.spawned[p]} | ${c.denied[p]} |`)
  lines.push(`| Agent/Task/Workflow calls refused while tripped | | ${c.denied.tool} |`)
  lines.push('', '| rule | now | warn | trip |', '| :- | -: | -: | -: |')
  for (const r of RULES) {
    const l = engine.config.limits[r]
    lines.push(`| ${r} | ${fmt(m[r])} | ${l.warn || 'off'} | ${l.trip || 'off'} |`)
  }
  if (incident) {
    lines.push('', `Stops: ${incident.stops.length ? '' : 'none issued.'}`)
    for (const s of incident.stops) lines.push(`- TaskStop ${s.id} (attempt ${s.attempt}): ${s.ok ? 'ok' : 'failed'} ${s.detail}`)
    lines.push(`Stop verified: ${incident.stopVerified === null ? 'not yet' : incident.stopVerified ? 'yes, nothing left running' : 'NO, agents still running'}`)
  }
  if (a.configErrors.length) lines.push('', 'Config problems (defaults used):', ...a.configErrors.map((e) => `- ${e}`))
  return lines.join('\n')
}
