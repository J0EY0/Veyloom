import { useSyncExternalStore } from 'react'
import type { TurnEvent } from '@/api/types'

// What the room has seen of a running turn since it was opened: the text
// the agent is saying now (not yet stored as a message), the tool it is in,
// and the events themselves for the activity block and the drawer. It is
// display state, not truth: the hub stores the text at each tool call and
// the transcript keeps every event.

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
}

// eventCap bounds memory per turn (docs/webui.md §5.2); older events fall off.
const eventCap = 500

const turns = new Map<string, LiveTurn>()
const listeners = new Set<() => void>()

function emit() {
  listeners.forEach((listener) => listener())
}

export function applyTurnEvent(turnId: string, event: TurnEvent) {
  const old = turns.get(turnId) ?? { turnId, text: '', events: [] }
  const events = old.events.length >= eventCap ? [...old.events.slice(1), event] : [...old.events, event]
  const next: LiveTurn = { ...old, events }
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
      next.tool = event.tool
      break
    case 'tool_result':
      next.tool = undefined
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
  turns.set(turnId, next)
  emit()
}

export function clearLiveTurn(turnId: string) {
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
  emit()
}
