import { useSyncExternalStore } from 'react'
import type { TurnEvent } from '@/api/types'

// What the room knows of a running turn: the text the agent is saying now
// (not yet stored as a message), the tool it is in, and the events
// themselves for the activity block and the drawer. It is display state,
// not truth: the hub stores the text at each tool call and the transcript
// keeps every event. What the page heard live is laid over what it read of
// the transcript (seedLiveTurn), the hub's numbers on the events (seq)
// telling the two apart, so a page opened while the turn runs, or one whose
// stream dropped, still has the whole of it.

export interface LiveTurn {
  turnId: string
  // Text streamed since the last tool boundary; the hub has not stored it yet.
  text: string
  // The tool running now, between its call and its result.
  tool?: string
  // The runtime is compacting the session: summarising its older part to
  // make room. It takes a while and says nothing meanwhile, so the room
  // says so instead of looking stuck.
  compacting?: boolean
  events: TurnEvent[]
  // The number of the last event taken in, 0 before one with a number.
  seq: number
}

// eventCap bounds memory per turn (docs/webui.md §5.2). Past it the oldest
// text goes first: words stream in pieces, many to a sentence, and the
// steps are what the activity block counts.
const eventCap = 2000

const turns = new Map<string, LiveTurn>()
// Turns over: a transcript read late does not bring them back.
const ended = new Set<string>()
const listeners = new Set<() => void>()

function emit() {
  listeners.forEach((listener) => listener())
}

function blank(turnId: string): LiveTurn {
  return { turnId, text: '', events: [], seq: 0 }
}

function withEvent(events: TurnEvent[], event: TurnEvent): TurnEvent[] {
  const next = [...events, event]
  if (next.length > eventCap) {
    const text = next.findIndex((e) => e.kind === 'text')
    next.splice(Math.max(text, 0), 1)
  }
  return next
}

// step is what one event makes of a live turn.
function step(old: LiveTurn, event: TurnEvent): LiveTurn {
  const next: LiveTurn = { ...old, events: withEvent(old.events, event), seq: Math.max(old.seq, event.seq ?? 0) }
  switch (event.kind) {
    case 'text':
      next.text = old.text + (event.text ?? '')
      break
    case 'tool_call':
      // The hub stores the text said so far as a message at this point.
      next.text = ''
      next.tool = event.tool
      break
    case 'approval_request':
      next.text = ''
      // A request the runtime's own reviewer settled waits for nobody.
      if (!event.reviewer) next.tool = event.tool
      break
    case 'tool_result':
      next.tool = undefined
      break
    case 'steer':
      // Passed what people said: the hub stores what the agent said before
      // as a message of its own, and what follows answers that too.
      next.text = ''
      break
    case 'compaction':
      next.compacting = event.phase === 'start'
      break
    default:
      break
  }
  // Anything the agent says or does means the compaction is behind it,
  // whether or not its end was heard.
  if (event.kind === 'text' || event.kind === 'tool_call') next.compacting = false
  return next
}

export function applyTurnEvent(turnId: string, event: TurnEvent) {
  const old = turns.get(turnId) ?? blank(turnId)
  // Taken in already, from the transcript read under the stream.
  if (event.seq !== undefined && event.seq <= old.seq) return
  turns.set(turnId, step(old, event))
  emit()
}

// seedLiveTurn lays the events of a running turn's transcript, as far as
// it was read, under those the page heard live: one heard both ways is
// taken once, by its number, and the turn is played again in order.
export function seedLiveTurn(turnId: string, events: TurnEvent[]) {
  if (ended.has(turnId)) return
  const byNumber = new Map<number, TurnEvent>()
  for (const event of [...events, ...(turns.get(turnId)?.events ?? [])]) {
    if (event.seq !== undefined) byNumber.set(event.seq, event)
  }
  if (byNumber.size === 0) return
  let next = blank(turnId)
  for (const event of [...byNumber.values()].sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))) next = step(next, event)
  turns.set(turnId, next)
  emit()
}

export function clearLiveTurn(turnId: string) {
  ended.add(turnId)
  if (turns.delete(turnId)) emit()
}

export function getLiveTurn(turnId: string | undefined): LiveTurn | undefined {
  return turnId ? turns.get(turnId) : undefined
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// useLiveTurn follows one turn; undefined when it is not running or
// nothing has arrived for it.
export function useLiveTurn(turnId: string | undefined): LiveTurn | undefined {
  return useSyncExternalStore(
    subscribe,
    () => getLiveTurn(turnId),
    () => getLiveTurn(turnId),
  )
}

// resetLiveTurns forgets everything; for tests.
export function resetLiveTurns() {
  turns.clear()
  ended.clear()
  emit()
}
