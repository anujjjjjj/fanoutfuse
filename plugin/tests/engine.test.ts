// Unit tests of the config parser and the engine arithmetic.
import { expect, test } from 'claude-code/testing'
import { parseConfig } from '../src/config.ts'
import { classify, Engine } from '../src/engine.ts'

test('defaults parse cleanly', () => {
  const { config, errors } = parseConfig({}, {})
  expect(errors).toEqual([])
  expect(config.limits.total_spawns).toEqual({ warn: 60, trip: 120 })
  expect(config.limits.quota_burn).toEqual({ warn: 2, trip: 5 })
  expect(config.stopRunning).toBe(true)
})

test('env overrides options, options override defaults', () => {
  const { config } = parseConfig({ total_spawns_trip: 50, spawn_rate_trip: 9 }, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '7' })
  expect(config.limits.total_spawns.trip).toBe(7)
  expect(config.limits.spawn_rate.trip).toBe(9)
})

test('warn above trip is clamped and reported', () => {
  const { config, errors } = parseConfig({}, { FANOUTFUSE_MAX_DEPTH_WARN: '9' })
  expect(config.limits.max_depth.warn).toBe(4)
  expect(errors.length).toBe(1)
})

test('lowering only the trip pulls the default warn down quietly', () => {
  const { config, errors } = parseConfig({}, { FANOUTFUSE_TOTAL_SPAWNS_TRIP: '3' })
  expect(config.limits.total_spawns).toEqual({ warn: 3, trip: 3 })
  expect(errors).toEqual([])
})

test('booleans accept common spellings and reject others', () => {
  expect(parseConfig({}, { FANOUTFUSE_STOP_RUNNING: 'off' }).config.stopRunning).toBe(false)
  const bad = parseConfig({}, { FANOUTFUSE_STOP_RUNNING: 'maybe' })
  expect(bad.config.stopRunning).toBe(true)
  expect(bad.errors.length).toBe(1)
})

test('FANOUTFUSE_HOME wins over HOME', () => {
  expect(parseConfig({}, { HOME: '/h' }).config.home).toBe('/h/.fanoutfuse')
  expect(parseConfig({}, { HOME: '/h', FANOUTFUSE_HOME: '/x' }).config.home).toBe('/x')
})

test('classify', () => {
  const base = { toolUseId: 't', fork: false, background: false }
  expect(classify({ ...base, subagentType: 'general-purpose' })).toBe('agent')
  expect(classify({ ...base, subagentType: 'Explore' })).toBe('agent')
  expect(classify({ ...base, subagentType: 'workflow-subagent' })).toBe('workflow')
  expect(classify({ ...base, subagentType: 'fork', fork: true })).toBe('fork')
})

test('growth_factor needs enough recent spawns before it counts', () => {
  const e = new Engine(parseConfig({}, { FANOUTFUSE_SPAWN_RATE_TRIP: '0', FANOUTFUSE_SPAWN_BURST_TRIP: '0', FANOUTFUSE_BREADTH_PER_PARENT_TRIP: '0', FANOUTFUSE_RUNNING_CONCURRENT_TRIP: '0' }).config)
  const r = { toolUseId: 't', subagentType: 'workflow-subagent', fork: false, background: false }
  e.record(r, 'p1', 0)
  e.record(r, 'p2', 0)
  // 2 spawns 30-60 s ago, then 19 in the last 30 s: factor 9.5 but n=19 < 20
  for (let i = 0; i < 19; i++) e.record(r, `c${i}`, 40_000)
  expect(e.metrics(45_000).growth_factor).toBe(9.5)
  expect(e.evaluate(45_000).find((v) => v.rule === 'growth_factor')?.level).toBe('warn')
  const d = e.preSpawn(r, 45_000)
  expect(d.allow).toBe(false)
})

test('quota burn uses at least one minute of elapsed time', () => {
  const e = new Engine(parseConfig().config)
  e.measureQuota(10, 'w', 0)
  e.measureQuota(20, 'w', 1000)
  expect(e.quotaBurn(1000)).toBe(10)
})

test('quota burn measures from the reading at the window start', () => {
  const e = new Engine(parseConfig().config)
  e.measureQuota(10, 'w', 0)
  e.measureQuota(11, 'w', 4 * 60_000)
  e.measureQuota(20, 'w', 10 * 60_000)
  // window 5 min: baseline is the 11% reading at minute 4 → 9 pts over 6 min
  expect(e.quotaBurn(10 * 60_000)).toBe(1.5)
})
