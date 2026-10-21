// Every way a subagent starts, driven through the real hooks module.
import { expect, test } from 'claude-code/testing'
import { agentSpawn, workflowSpawn, world } from './harness.ts'

test('Agent tool spawn below the limits starts and is counted', async ($, on) => {
  world(on)
  const r = await $.agent.spawn(agentSpawn('t1'))
  expect(r.agentId).toBe('a1')
  const s = await $.command.run({ command: 'fanoutfuse', args: 'status' })
  expect(s.text).toContain('armed')
  expect(s.text).toContain('spawned agent=1 workflow=0 fork=0')
})

test('Agent path: the spawn that reaches total_spawns trip is refused, and later ones too', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '3', FANOUTFUSE_TOTAL_SPAWNS_WARN: '2' })
  expect((await $.agent.spawn(agentSpawn('t1'))).agentId).toBe('a1')
  expect((await $.agent.spawn(agentSpawn('t2'))).agentId).toBe('a2')
  const third = await $.agent.spawn(agentSpawn('t3'))
  expect(third.deny).toContain('total_spawns: 3 >= 3')
  const fourth = await $.agent.spawn(agentSpawn('t4'))
  expect(fourth.deny).toContain('FanoutFuse tripped total_spawns')
  expect(w.spawned).toBe(2)
})

test('Task alias: a Task tool call is refused while tripped', async ($, on) => {
  world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1' })
  expect((await $.agent.spawn(agentSpawn('t1'))).deny).toContain('total_spawns')
  const r = await $.tool.call({ tool: 'Task', prompt: 'x', description: 'x' } as any)
  expect(r.deny).toContain('FanoutFuse tripped')
})

test('Agent tool call is refused while tripped', async ($, on) => {
  world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1' })
  await $.agent.spawn(agentSpawn('t1'))
  const r = await $.tool.call({ tool: 'Agent', prompt: 'x', description: 'x' } as any)
  expect(r.deny).toContain('FanoutFuse tripped')
})

test('Workflow path: each workflow agent spawn counts, and the crossing one is refused', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '3' })
  await $.tool.call({ tool: 'Workflow', script: 'x' } as any)
  const results = []
  for (const i of [1, 2, 3, 4]) results.push(await $.agent.spawn(workflowSpawn('wf1', { description: `w${i}` })))
  expect(results[0]!.agentId).toBe('a1')
  expect(results[1]!.agentId).toBe('a2')
  expect(results[2]!.deny).toContain('total_spawns')
  expect(results[3]!.deny).toContain('FanoutFuse tripped')
  expect(w.spawned).toBe(2)
  const s = await $.command.run({ command: 'fanoutfuse', args: 'report' })
  expect(s.text).toContain('| workflow | 2 | 2 |')
})

test('Workflow tool call is refused while tripped (covers ultracode, which runs as workflows)', async ($, on) => {
  world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1' })
  await $.agent.spawn(workflowSpawn('wf1'))
  const r = await $.tool.call({ tool: 'Workflow', script: 'x' } as any)
  expect(r.deny).toContain('FanoutFuse tripped')
})

test('Fork path is counted and refused after a trip', async ($, on) => {
  world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '2' })
  expect((await $.agent.spawn(agentSpawn('t1', { fork: true, subagentType: 'fork' }))).agentId).toBe('a1')
  expect((await $.agent.spawn(agentSpawn('t2', { fork: true, subagentType: 'fork' }))).deny).toContain('total_spawns')
  const s = await $.command.run({ command: 'fanoutfuse', args: 'report' })
  expect(s.text).toContain('| fork | 1 | 1 |')
})

test('Nested Agent spawns: depth follows parentAgentId and max_depth trips', async ($, on) => {
  world(on, { FANOUTFUSE_MAX_DEPTH_TRIP: '3' })
  const d1 = await $.agent.spawn(agentSpawn('t1'))
  const d2 = await $.agent.spawn(agentSpawn('t2', { parentAgentId: d1.agentId }))
  expect(d2.agentId).toBe('a2')
  const d3 = await $.agent.spawn(agentSpawn('t3', { parentAgentId: d2.agentId }))
  expect(d3.deny).toContain('max_depth: 3 >= 3')
})

test('breadth_per_parent counts Agent-tool children, not a workflow fanning out', async ($, on) => {
  world(on, { FANOUTFUSE_BREADTH_PER_PARENT_TRIP: '3' })
  for (const i of [1, 2, 3, 4, 5]) expect((await $.agent.spawn(workflowSpawn('wf1', { description: `w${i}` }))).agentId).toBe(`a${i}`)
  expect((await $.agent.spawn(agentSpawn('t1'))).agentId).toBe('a6')
  expect((await $.agent.spawn(agentSpawn('t2'))).agentId).toBe('a7')
  expect((await $.agent.spawn(agentSpawn('t3'))).deny).toContain('breadth_per_parent')
})

test('a workflow of 16 agents at once stays under the default limits', async ($, on) => {
  const w = world(on)
  await $.tool.call({ tool: 'Workflow', script: 'x' } as any)
  for (let i = 1; i <= 16; i++) expect((await $.agent.spawn(workflowSpawn('wf1', { description: `w${i}` }))).agentId).toBe(`a${i}`)
  expect(w.toasts.filter((t) => t.includes('tripped'))).toEqual([])
})

test('spawn_burst trips on a fast burst', async ($, on) => {
  world(on, { FANOUTFUSE_SPAWN_BURST_TRIP: '4' })
  for (const i of [1, 2, 3]) expect((await $.agent.spawn(workflowSpawn('wf1'))).agentId).toBe(`a${i}`)
  expect((await $.agent.spawn(workflowSpawn('wf1'))).deny).toContain('spawn_burst')
})

test('running_concurrent counts agents until their turn completes', async ($, on) => {
  const w = world(on, { FANOUTFUSE_RUNNING_CONCURRENT_TRIP: '3' })
  await $.agent.spawn(workflowSpawn('wf1'))
  await $.agent.spawn(workflowSpawn('wf1'))
  await $.turn.complete({ turnId: 'x', agentId: 'a1', answer: '', durationMs: 1, isAborted: false, reason: 'answer' } as any)
  expect((await $.agent.spawn(workflowSpawn('wf1'))).agentId).toBe('a3')
  expect((await $.agent.spawn(workflowSpawn('wf1'))).deny).toContain('running_concurrent')
  expect(w.spawned).toBe(3)
})

test('tools that do not spawn pass through while tripped', async ($, on) => {
  world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '1' })
  await $.agent.spawn(agentSpawn('t1'))
  const r = await $.tool.call({ tool: 'Bash', command: 'ls' } as any)
  expect(r.result).toBe('ok')
})

test('the Go CLI fuse file (fanoutfuse stop) refuses spawns on every path', async ($, on) => {
  const w = world(on)
  w.fuses.add('ALL')
  expect((await $.agent.spawn(agentSpawn('t1'))).deny).toContain('fuse is set')
  expect((await $.agent.spawn(workflowSpawn('wf1'))).deny).toContain('fuse is set')
  expect((await $.tool.call({ tool: 'Workflow', script: 'x' } as any)).deny).toContain('fuse is set')
  expect(w.spawned).toBe(0)
})

// Found in the real Workflow run: a workflow raises its agents' spawns
// concurrently, so each must be counted the moment it is allowed, not when
// core reports it started.
test('concurrent workflow spawns are counted as they are allowed (no race past the trip)', async ($, on) => {
  const w = world(on, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '3' })
  const results = await Promise.all([1, 2, 3, 4, 5].map((i) => $.agent.spawn(workflowSpawn('wf1', { description: `w${i}` }))))
  expect(results.filter((r) => r.agentId).length).toBe(2)
  expect(results.filter((r) => r.deny).length).toBe(3)
  expect(w.spawned).toBe(2)
})
