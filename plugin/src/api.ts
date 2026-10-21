// The slice of the Claude Code mods API this plugin uses, typed structurally
// (from the claude-code module's declarations, v2.1.277+). Kept local so the
// hooks module has relative imports only, as the mods loader requires.

export type AgentInfo = { id: string; description: string; type: string; status: string; parentId?: string }

export type AgentSpawnInput = {
  tool_use_id: string
  prompt: string
  description: string
  subagentType: string
  parentModel: string
  parentAgentId?: string
  background: boolean
  fork: boolean
  name?: string
}
export type AgentSpawnResult = { model: string; agentId?: string; deny?: undefined } | { deny: string; model?: undefined; agentId?: undefined }

export type ToolCallInput = { tool: string; tool_use_id: string; agentId?: string; [arg: string]: unknown }
export type ToolCallResult = { deny: string; result?: undefined; isError?: undefined; text?: undefined } | { result: unknown; text?: string; isError?: boolean; deny?: undefined }

export type RateLimit = { kind: string; percentUsed: number; resetsAt?: string }
export type SessionMeasureInput = { rateLimits: RateLimit[]; changed: string[] }

export type TurnUsage = { input_tokens: number; output_tokens: number; cache_read_input_tokens: number; cache_creation_input_tokens: number; model: string }
export type TurnCompleteInput = { answer: string; turnId: string; agentId?: string; reason: string; isAborted: boolean; usage?: TurnUsage }
export type TurnStartInput = { text: string; turnId: string }
export type CommandRunInput = { command: string; args: string }
export type CommandRunResult = { text?: string }

export type Api = {
  agent: { list: () => Promise<AgentInfo[]> }
  tool: { call: (input: { tool: string; [arg: string]: unknown }) => Promise<ToolCallResult> }
  turn: { abort: (input: { turnId: string }) => Promise<void> }
  session: { id: () => Promise<string> }
  fs: { write: (path: string, text: string) => Promise<void>; exists: (path: string) => Promise<boolean> }
  env: { get: (name: string) => Promise<string | undefined> }
  clock: { now: () => Promise<number>; after: (ms: number, fn: () => unknown) => { cancel: () => void } }
  ui: { toast: (text: string, options?: { timeoutMs?: number }) => void; log: (text: string) => void }
  command: { register: (spec: { name: string; description: string; argumentHint?: string; immediate?: true }) => Promise<unknown> }
}

export type Next<I, O> = (e: I) => Promise<O>
export type Hook<I, O> = ($: Api, e: I, next: Next<I, O>) => Promise<O> | O
export type Registration = { catch: (handler: (...args: any[]) => unknown) => unknown }
export type On = (event: string, hook: Hook<any, any>) => Registration
