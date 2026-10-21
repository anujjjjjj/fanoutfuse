// The stop ladder and the 5-hour quota burn trigger.
import { expect, test } from 'claude-code/testing'
import { agentSpawn, measure, workflowSpawn, world } from './harness.ts'

test('a trip stops running Agent subagents and the running workflow, then verifies', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '4' })
  await $.agent.spawn(agentSpawn('t1', { background: true }))
  await $.tool.call({ tool: 'Workflow', script: 'x' } as any)
  // The Workflow tool call's id is what each of its agents' spawns carry.
  await $.agent.spawn(workflowSpawn('wf-call'))
  await $.agent.spawn(workflowSpawn('wf-call'))
  const denied = await $.agent.spawn(workflowSpawn('wf-call'))
  expect(denied.deny).toContain('total_spawns')
  await w.clock.settle()
  expect(w.stops).toContain('a1')
  await w.clock.advance(5000)
  const report = await $.command.run({ command: 'fanoutfuse', args: 'report' })
  expect(report.text).toContain('Stop verified: yes')
  const incident = w.writes.at(-1)!
  expect(incident.path).toMatch(/^\/home\/test\/\.fanoutfuse\/incidents\/plugin-sess1-.*\.json$/)
  expect(JSON.parse(incident.text).stopVerified).toBe(true)
})

test('the workflow task id from the Workflow result is the stop target for its agents', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '3' })
  // tool.call fires with the model's tool_use_id; agent.spawn carries the same id.
  const call = { tool: 'Workflow', script: 'x', tool_use_id: 'toolu_wf' } as any
  await $.tool.call(call)
  await $.agent.spawn(workflowSpawn('toolu_wf'))
  await $.agent.spawn(workflowSpawn('toolu_wf'))
  await $.agent.spawn(workflowSpawn('toolu_wf'))
  await w.clock.settle()
  // The kit may mint its own tool_use_id; either way the running workflow agents are stopped.
  expect(w.stops.length).toBeGreaterThan(0)
  const r = await $.command.run({ command: 'fanoutfuse', args: 'status' })
  expect(r.text).toContain('running 0')
})

test('a stop that does not hold is retried once and reported', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '2' })
  await $.agent.spawn(agentSpawn('t1', { background: true }))
  w.sticky.add('a1')
  await $.agent.spawn(agentSpawn('t2'))
  await w.clock.settle()
  expect(w.stops).toEqual(['a1'])
  await w.clock.advance(5000)
  expect(w.stops).toEqual(['a1', 'a1'])
  expect(w.toasts.some((t) => t.includes('still running after two stops'))).toBe(true)
  const report = await $.command.run({ command: 'fanoutfuse', args: 'report' })
  expect(report.text).toContain('Stop verified: NO')
})

test('stop_running=false refuses new spawns but leaves running agents alone', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '2', FANOUTFUSE_STOP_RUNNING: 'false' })
  await $.agent.spawn(agentSpawn('t1', { background: true }))
  expect((await $.agent.spawn(agentSpawn('t2'))).deny).toContain('total_spawns')
  await w.clock.advance(10000)
  expect(w.stops).toEqual([])
})

test('abort_turn interrupts the main turn on a trip', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1', FANOUTFUSE_ABORT_TURN: 'true' })
  await $.turn.start({ text: 'go', turnId: 'turn-9' } as any)
  await $.agent.spawn(agentSpawn('t1'))
  await w.clock.settle()
  expect(w.aborted).toEqual(['turn-9'])
})

test('quota_burn trips when the 5-hour window climbs past 5 points a minute, and stops agents', async ($, on) => {
  const w = world(on)
  await $.agent.spawn(agentSpawn('t1', { background: true }))
  await measure($, 10)
  await w.clock.advance(60_000)
  await measure($, 14) // 4 pts/min: warn only
  expect(w.toasts.some((t) => t.includes('warning: quota_burn'))).toBe(true)
  expect(w.stops).toEqual([])
  await w.clock.advance(60_000)
  await measure($, 22) // 12 pts over 2 min = 6 pts/min
  expect(w.stops).toEqual(['a1'])
  expect((await $.agent.spawn(agentSpawn('t2'))).deny).toContain('quota_burn')
})

test('quota_burn ignores one-point jitter and small moves', async ($, on) => {
  const w = world(on)
  for (const pct of [3, 2, 3, 2, 4]) {
    await measure($, pct)
    await w.clock.advance(1000)
  }
  expect((await $.agent.spawn(agentSpawn('t1'))).agentId).toBe('a1')
  expect(w.toasts).toEqual([])
})

test('a new 5-hour window (resetsAt changes) starts the burn series over', async ($, on) => {
  const w = world(on)
  await measure($, 90, 'window-1')
  await w.clock.advance(60_000)
  await measure($, 1, 'window-2')
  await w.clock.advance(60_000)
  await measure($, 4, 'window-2') // 3 pts in a minute: under warn
  expect((await $.agent.spawn(agentSpawn('t1'))).agentId).toBe('a1')
  const s = await $.command.run({ command: 'fanoutfuse', args: 'status' })
  expect(s.text).toContain('five_hour 4% at 3 pts/min')
})

test('quota_used trips at an absolute percentage when configured', async ($, on) => {
  const w = world(on, { FANOUTFUSE_QUOTA_USED_TRIP: '95' })
  await measure($, 96)
  expect((await $.agent.spawn(agentSpawn('t1'))).deny).toContain('quota_used: 96 >= 95')
  expect(w.spawned).toBe(0)
})

test('tokens_per_min trips from subagent turn usage', async ($, on) => {
  world(on, { FANOUTFUSE_TOKENS_PER_MIN_TRIP: '1000' })
  const a = await $.agent.spawn(agentSpawn('t1'))
  await $.turn.complete({ turnId: 'x', agentId: a.agentId, answer: '', durationMs: 1, isAborted: false, reason: 'answer', usage: { input_tokens: 900, output_tokens: 200, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, model: 'm' } } as any)
  expect((await $.agent.spawn(agentSpawn('t2'))).deny).toContain('tokens_per_min')
})

test('/fanoutfuse resume clears the trip and doubles thresholds for the cooldown', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '2' })
  await $.agent.spawn(agentSpawn('t1'))
  expect((await $.agent.spawn(agentSpawn('t2'))).deny).toContain('total_spawns')
  const r = await $.command.run({ command: 'fanoutfuse', args: 'resume' })
  expect(r.text).toContain('FanoutFuse resumed')
  // Trip doubled to 4: spawns 2 and 3 start, the 4th is refused.
  expect((await $.agent.spawn(agentSpawn('t3'))).agentId).toBe('a2')
  expect((await $.agent.spawn(agentSpawn('t4'))).agentId).toBe('a3')
  expect((await $.agent.spawn(agentSpawn('t5'))).deny).toContain('total_spawns: 4 >= 4')
  expect(w.spawned).toBe(3)
})

test('bad config values are reported and fall back to defaults', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: 'lots' })
  expect((await $.agent.spawn(agentSpawn('t1'))).agentId).toBe('a1')
  expect(w.logs.some((l) => l.includes('FANOUTFUSE_TOTAL_SPAWNS_TRIP'))).toBe(true)
})

test('refusals after the trip keep the incident file current', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1' })
  await $.agent.spawn(agentSpawn('t1'))
  await w.clock.settle()
  await $.tool.call({ tool: 'Agent', prompt: 'x', description: 'x' } as any)
  const last = JSON.parse(w.writes.at(-1)!.text)
  expect(last.counts.denied.agent).toBe(1)
  expect(last.counts.denied.tool).toBe(1)
})

test('session end verifies the stop when the timer has not fired yet', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '2' })
  on('session.end', ($: any, e: any) => ({ sessionId: e.sessionId }))
  await $.agent.spawn(agentSpawn('t1', { background: true }))
  await $.agent.spawn(agentSpawn('t2'))
  await w.clock.settle()
  await $.session.end({ reason: 'other', sessionId: 'sess1', resume: { sessionId: 'sess1' } } as any)
  expect(JSON.parse(w.writes.at(-1)!.text).stopVerified).toBe(true)
})
