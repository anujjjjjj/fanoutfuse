// Typed configuration for the FanoutFuse plugin.
//
// Precedence, lowest to highest: built-in defaults, plugin options
// (plugin.json userConfig, set through /plugin configure or /config), then
// FANOUTFUSE_<RULE>_WARN / FANOUTFUSE_<RULE>_TRIP environment variables, the
// same names the Go CLI reads.

export const RULES = [
  'spawn_rate',
  'spawn_burst',
  'total_spawns',
  'max_depth',
  'running_concurrent',
  'tokens_per_min',
  'growth_factor',
  'breadth_per_parent',
  'quota_burn',
  'quota_used',
] as const

export type RuleName = (typeof RULES)[number]

/** A rule's thresholds. 0 disables that level. A metric at or above `trip` trips. */
export type Limit = { readonly warn: number; readonly trip: number }

export type Config = {
  readonly limits: Readonly<Record<RuleName, Limit>>
  /** growth_factor only counts once this many spawns landed in the last 30 s. */
  readonly growthMinN: { readonly warn: number; readonly trip: number }
  /** Minutes of five_hour quota history the burn rate is measured over. */
  readonly quotaWindowMin: number
  /** Percentage points the five_hour window must move before quota_burn can fire. */
  readonly quotaMinDeltaPts: number
  /** On trip, stop subagents and workflows that are already running (TaskStop). */
  readonly stopRunning: boolean
  /** On trip, also abort the main loop's current turn. */
  readonly abortTurn: boolean
  /** How long after a stop the plugin re-checks that the agents are gone. */
  readonly verifyAfterMs: number
  /** Thresholds are doubled for this long after /fanoutfuse resume. */
  readonly cooldownMs: number
  /** Directory for incidents and the Go CLI's fuse files; '' means $HOME/.fanoutfuse. */
  readonly home: string
}

export type Options = Readonly<Record<string, string | number | boolean | readonly string[]>>
export type Env = Readonly<Record<string, string | undefined>>

export const DEFAULT_LIMITS: Readonly<Record<RuleName, Limit>> = {
  spawn_rate: { warn: 20, trip: 40 }, // spawns in the last 60 s
  spawn_burst: { warn: 12, trip: 24 }, // spawns in the last 10 s (a workflow runs up to 16 at once)
  total_spawns: { warn: 60, trip: 120 }, // spawns this session
  max_depth: { warn: 3, trip: 4 }, // nesting depth of Agent-tool spawns
  running_concurrent: { warn: 20, trip: 32 }, // subagents running now
  tokens_per_min: { warn: 0, trip: 0 }, // subagent tokens in the last 60 s (off)
  growth_factor: { warn: 3, trip: 4 }, // spawns last 30 s / spawns 30-60 s ago
  breadth_per_parent: { warn: 6, trip: 10 }, // Agent-tool children of one parent
  quota_burn: { warn: 2, trip: 5 }, // five_hour quota, percentage points per minute
  quota_used: { warn: 0, trip: 0 }, // five_hour quota, percent used (off)
}

export const DEFAULTS: Config = {
  limits: DEFAULT_LIMITS,
  growthMinN: { warn: 10, trip: 20 },
  quotaWindowMin: 5,
  quotaMinDeltaPts: 3,
  stopRunning: true,
  abortTurn: false,
  verifyAfterMs: 5000,
  cooldownMs: 10 * 60 * 1000,
  home: '',
}

export type ParseResult = { readonly config: Config; readonly errors: readonly string[] }

function num(raw: unknown, name: string, errors: string[], min = 0): number | undefined {
  if (raw === undefined || raw === '') return undefined
  const n = typeof raw === 'number' ? raw : Number(String(raw).trim())
  if (!Number.isFinite(n) || n < min) {
    errors.push(`${name}: ${JSON.stringify(raw)} is not a number >= ${min}; using the default`)
    return undefined
  }
  return n
}

function bool(raw: unknown, name: string, errors: string[]): boolean | undefined {
  if (raw === undefined || raw === '') return undefined
  if (typeof raw === 'boolean') return raw
  const s = String(raw).trim().toLowerCase()
  if (['1', 'true', 'yes', 'on'].includes(s)) return true
  if (['0', 'false', 'no', 'off'].includes(s)) return false
  errors.push(`${name}: ${JSON.stringify(raw)} is not a boolean; using the default`)
  return undefined
}

/**
 * Builds the config from plugin options and the environment. Invalid values
 * are reported in `errors` and fall back to the default; they never throw,
 * so a typo cannot disable the guard.
 */
export function parseConfig(options: Options = {}, env: Env = {}): ParseResult {
  const errors: string[] = []
  const limits = {} as Record<RuleName, Limit>
  for (const rule of RULES) {
    const up = rule.toUpperCase()
    const warnSet =
      num(env[`FANOUTFUSE_${up}_WARN`], `FANOUTFUSE_${up}_WARN`, errors) ?? num(options[`${rule}_warn`], `option ${rule}_warn`, errors)
    const tripSet =
      num(env[`FANOUTFUSE_${up}_TRIP`], `FANOUTFUSE_${up}_TRIP`, errors) ?? num(options[`${rule}_trip`], `option ${rule}_trip`, errors)
    let warn = warnSet ?? DEFAULT_LIMITS[rule].warn
    const trip = tripSet ?? DEFAULT_LIMITS[rule].trip
    if (warn > 0 && trip > 0 && warn > trip) {
      // Lowering only the trip pulls the default warn down with it; an explicit warn above trip is a mistake worth saying.
      if (warnSet !== undefined) errors.push(`${rule}: warn ${warn} is above trip ${trip}; warn set to trip`)
      warn = trip
    }
    limits[rule] = { warn, trip }
  }
  const pick = <T>(key: string, envKey: string, parse: (raw: unknown, name: string) => T | undefined, fallback: T): T =>
    parse(env[envKey], envKey) ?? parse(options[key], `option ${key}`) ?? fallback
  const n = (min: number) => (raw: unknown, name: string) => num(raw, name, errors, min)
  const b = (raw: unknown, name: string) => bool(raw, name, errors)
  const s = (raw: unknown) => (typeof raw === 'string' && raw.trim() !== '' ? raw.trim() : undefined)
  return {
    errors,
    config: {
      limits,
      growthMinN: DEFAULTS.growthMinN,
      quotaWindowMin: pick('quota_window_min', 'FANOUTFUSE_QUOTA_WINDOW_MIN', n(1), DEFAULTS.quotaWindowMin),
      quotaMinDeltaPts: pick('quota_min_delta_pts', 'FANOUTFUSE_QUOTA_MIN_DELTA_PTS', n(0), DEFAULTS.quotaMinDeltaPts),
      stopRunning: pick('stop_running', 'FANOUTFUSE_STOP_RUNNING', b, DEFAULTS.stopRunning),
      abortTurn: pick('abort_turn', 'FANOUTFUSE_ABORT_TURN', b, DEFAULTS.abortTurn),
      verifyAfterMs: pick('verify_after_ms', 'FANOUTFUSE_VERIFY_AFTER_MS', n(100), DEFAULTS.verifyAfterMs),
      cooldownMs: DEFAULTS.cooldownMs,
      home: s(env.FANOUTFUSE_HOME) ?? (s(env.HOME) ? `${s(env.HOME)}/.fanoutfuse` : ''),
    },
  }
}
