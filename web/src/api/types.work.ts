// Wire types for the project's task board, a piece of work's page and the
// usage page (docs/webui.md 4.20); see types.ts.

import type { TokenUsage, TurnKind, TurnStatus } from './types'

// Where a task stands: under way, its changes waiting to be merged, done.
export type TaskState = 'running' | 'merge' | 'done'

// What came of a task: merged in a commit, its branch reset with its work
// archived under a ref, or a page written in the wiki.
export interface TaskOutcome {
  kind: 'merged' | 'archived' | 'wiki'
  commit?: string
  ref?: string
  page?: string
}

// The task another one is part of.
export interface TaskRef {
  chain: string
  thread_number: number
  member_id: string
  title: string
}

// One member's part of a piece of work in one topic: a card of the board.
export interface Task {
  // The piece of work: the message a person began it with.
  chain: string
  thread_id: string
  thread_number: number
  member_id: string
  // What the member was asked, in a line; empty when nothing says.
  title: string
  // The task a person asked for, when this one was handed on.
  work?: TaskRef
  // How many tasks this one handed on, and how many of them are done.
  parts?: { done: number; total: number }
  state: TaskState
  // A request of one of its turns waits for a person.
  waiting?: boolean
  started_at: string
  ended_at?: string
  outcome?: TaskOutcome
  turns: number
}

export interface TasksResponse {
  tasks: Task[]
}

// A stretch a turn waited for a person; to is absent while it still does.
export interface WorkWait {
  from: string
  to?: string
}

// What a turn of a piece of work did: began its member's task; began the
// task a person asked for and handed parts on; handed work on later;
// summed up what came of it; or went on, woken again.
export type WorkTurnKind = 'task' | 'split' | 'handoff' | 'sumup' | 'continue'

export interface WorkTurn {
  id: string
  member_id: string
  thread_id: string
  thread_number: number
  status: TurnStatus
  error?: string
  started_at: string
  ended_at?: string
  usage: TokenUsage
  kind: WorkTurnKind
  title?: string
  // The members it handed work on to.
  woke?: string[]
  // It only handed work on: a quieter row.
  relay?: boolean
  waits?: WorkWait[]
  waited_ms: number
  files: number
}

// What became of branches the work changed.
export interface WorkEvent {
  kind: 'merged' | 'reset'
  commit?: string
  ref?: string
  // Whose work it carried or archived.
  members: string[]
  at: string
}

// A piece of work: the turns a person's message set going.
export interface Work {
  chain: string
  room_id: string
  thread_id: string
  thread_number: number
  // What the person asked, in a line, and whole.
  title: string
  ask: string
  asked_by?: string
  // The members the person asked.
  asked: string[]
  running: boolean
  started_at: string
  ended_at?: string
  turns: WorkTurn[]
  usage: TokenUsage
  waited_ms: number
  events: WorkEvent[]
}

export interface WorkResponse {
  work: Work
}

// How far back the usage page looks: today turn by turn, a week or a month
// day by day.
export type UsageRange = 'today' | '7d' | '30d'

// A turn, or a day's turns.
export interface UsagePoint {
  at: string
  tokens: number
  output: number
  duration_ms: number
  turns: number
  // For a turn: which, whose, where.
  turn_id?: string
  member_id?: string
  member?: string
  room_id?: string
  thread_number?: number
}

export interface UsageRuntime {
  runtime: string
  tokens: number
  turns: number
}

export interface UsageMember {
  member_id: string
  // The agent it is, whose picture it wears; absent once it is gone.
  agent_id?: string
  name: string
  model?: string
  runtime: string
  project_id: string
  project_name: string
  tokens: number
  turns: number
}

// A piece of work a person asked for (chain), or a project's setup or its
// wiki's upkeep (kind).
export interface UsageWork {
  chain?: string
  kind: TurnKind
  room_id: string
  project_name: string
  thread_number?: number
  title?: string
  tokens: number
  turns: number
}

export interface Usage {
  range: UsageRange
  step: 'turn' | 'day'
  total: TokenUsage
  turns: number
  points: UsagePoint[]
  runtimes: UsageRuntime[]
  members: UsageMember[]
  works: UsageWork[]
}

export interface UsageResponse {
  usage: Usage
}
