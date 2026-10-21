// Stubs for everything Claude Code answers beneath the plugin, so each test
// drives the real hooks module through the same events a session raises.

import { mock } from 'claude-code/testing'

export type World = {
  clock: { advance: (ms: number) => Promise<void>; settle: () => Promise<void>; now: () => number }
  /** Agents `$.agent.list()` reports; tests flip statuses. */
  agents: { id: string; type: string; status: string; description: string; parentId?: string }[]
  stops: string[]
  /** TaskStop ids that leave the agent running (a stop that does not hold). */
  sticky: Set<string>
  toasts: string[]
  logs: string[]
  writes: { path: string; text: string }[]
  aborted: string[]
  fuses: Set<string>
  spawned: number
  workflowTaskId: string
}

export const BASE = {
  prompt: 'p',
  description: 'd',
  provider: { plugin: 'engine', tier: 'core' },
  parentModel: 'claude-test',
  background: false,
  fork: false,
} as const

export function agentSpawn(toolUseId: string, extra: Record<string, unknown> = {}) {
  return { ...BASE, tool_use_id: toolUseId, subagentType: 'general-purpose', ...extra }
}

export function workflowSpawn(toolUseId: string, extra: Record<string, unknown> = {}) {
  return { ...BASE, tool_use_id: toolUseId, subagentType: 'workflow-subagent', ...extra }
}

export function world(on: any, env: Record<string, string> = {}): World {
  const w: World = {
    clock: mock.clock(on, { now: 1_000_000 }) as any,
    agents: [],
    stops: [],
    sticky: new Set(),
    toasts: [],
    logs: [],
    writes: [],
    aborted: [],
    fuses: new Set(),
    spawned: 0,
    workflowTaskId: 'wtask1',
  }
  mock.env(on, { HOME: '/home/test', ...env })
  on('session.id', () => ({ value: 'sess1' }))
  on('session.start', () => ({ cwd: '/work' }))
  on('command.register', () => ({ value: undefined }))
  on('ui.toast', ($: any, e: any) => (w.toasts.push(e.text), { value: undefined }))
  on('ui.log', ($: any, e: any) => (w.logs.push(e.text), { value: undefined }))
  on('fs.exists', ($: any, e: any) => ({ value: [...w.fuses].some((f) => String(e.path).endsWith(`/fuse/${f}`)) }))
  on('fs.write', ($: any, e: any) => (w.writes.push({ path: e.path, text: e.text }), { value: undefined }))
  on('turn.abort', ($: any, e: any) => (w.aborted.push(e.turnId), { value: undefined }))
  on('turn.start', ($: any, e: any) => ({ turnId: e.turnId }))
  on('turn.complete', () => ({ text: '' }))
  on('session.measure', ($: any, e: any) => ({ changed: e.changed }))
  on('agent.list', () => ({ value: w.agents.map((a) => ({ ...a })) }))
  // Core's side of a spawn: start it and list it as running.
  on('agent.spawn', ($: any, e: any) => {
    w.spawned++
    const agentId = `a${w.spawned}`
    if (e.subagentType !== 'workflow-subagent') w.agents.push({ id: agentId, type: e.subagentType, status: 'running', description: e.description, parentId: e.parentAgentId })
    return { model: 'haiku', agentId }
  })
  on('tool.call', ($: any, e: any) => {
    if (e.tool === 'TaskStop') {
      w.stops.push(e.task_id)
      if (!w.sticky.has(e.task_id)) for (const a of w.agents) if (a.id === e.task_id) a.status = 'killed'
      return { result: { message: `Successfully stopped task: ${e.task_id}` }, text: 'stopped' }
    }
    if (e.tool === 'Workflow') return { result: { status: 'async_launched', taskId: w.workflowTaskId, taskType: 'local_workflow' }, text: 'launched' }
    if (e.tool === 'Agent' || e.tool === 'Task') return { result: { status: 'async_launched', agentId: 'x' }, text: 'launched' }
    return { result: 'ok', text: 'ok' }
  })
  return w
}

export async function measure($: any, pct: number, resetsAt = '2026-10-18T06:30:00.000Z') {
  return $.session.measure({ context: { window: 200000 }, rateLimits: [{ kind: 'five_hour', percentUsed: pct, resetsAt }], changed: ['rateLimits'] })
}
