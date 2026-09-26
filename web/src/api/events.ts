import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { setConnectionStatus } from '@/lib/connection'
import { applyTurnEvent, clearLiveTurn } from '@/lib/liveTurns'
import { connectRoomEvents } from '@/lib/roomSocket'
import { applyApproval } from './approvals'
import { refreshBranches } from './branches'
import { applyRoomMessage, applyTurn } from './messages'
import { topicKeys } from './topics'
import { applyRunningTurn } from './turns'
import type { RoomEvent } from './types'
import { projectKeys } from './projects'
import { refresh } from './refresh'
import { invalidateUpkeep } from './upkeep'
import { refreshRelayHolds } from './relays'
import { invalidateWiki } from './wiki'
import { workKeys } from './work'

// applyRoomEvent writes one live event into the query cache, which is the
// only store the UI reads (docs/webui.md §5.2).
export function applyRoomEvent(client: QueryClient, event: RoomEvent) {
  // The task board and the pieces of work move with the room's turns, its
  // requests, and the notes of merges and resets.
  if (event.kind !== 'turn_event' && event.kind !== 'wiki_changed' && event.kind !== 'inbox_read') {
    if (event.kind !== 'message' || event.message.sender_kind === 'system') {
      refresh(client, { queryKey: workKeys.tasks(event.room_id) })
      refresh(client, { queryKey: workKeys.all })
    }
  }
  switch (event.kind) {
    case 'message':
      applyRoomMessage(client, event.message, event.thread)
      // A note of the system's may be one the project now names: its wiki
      // or setup topic, its offer of a wiki maintainer (docs/design.md
      // 5.16), the setup steps waiting for a person (5.21).
      if (event.message.sender_kind === 'system') {
        refresh(client, { queryKey: projectKeys.all })
        // Or one telling of a wake a limit held back (5.22).
        if (event.message.thread_id) refreshRelayHolds(client, event.message.thread_id)
      }
      break
    case 'turn_started':
      applyTurn(client, event.turn, event.work)
      applyRunningTurn(client, event.turn)
      refresh(client, { queryKey: topicKeys.running })
      invalidateUpkeep(client)
      refreshBranches(client)
      break
    case 'turn_finished':
      applyTurn(client, event.turn, event.work)
      applyRunningTurn(client, event.turn)
      // Its messages have arrived by now; the live copy is stale.
      clearLiveTurn(event.turn.id)
      refresh(client, { queryKey: topicKeys.running })
      invalidateUpkeep(client)
      // What the member did shows on its branch.
      refreshBranches(client)
      break
    case 'turn_trust':
      // Whether a person lets the rest of the turn's requests through.
      applyTurn(client, event.turn)
      applyRunningTurn(client, event.turn)
      break
    case 'turn_event':
      applyTurnEvent(event.turn_id, event.turn_event)
      break
    case 'approval_requested':
    case 'approval_decided':
      applyApproval(client, event.approval)
      break
    case 'wiki_changed':
      invalidateWiki(client, event.scope === 'library' ? '' : (event.project_id ?? ''))
      break
    default:
      break
  }
}

// useRoomEvents keeps the room's live stream flowing into the cache while
// the room is open. Each time the stream opens, everything on screen is
// read again: what happened before it was listening reached no one.
export function useRoomEvents(roomId: string) {
  const client = useQueryClient()
  useEffect(() => {
    if (roomId === '') return
    const socket = connectRoomEvents(roomId, {
      onEvent: (event) => applyRoomEvent(client, event),
      // Anything may have changed before the stream listened: a turn that
      // ended while the page loaded, or over a drop, the server itself (a
      // restart, a wiped database). Refetch every query on screen, not just
      // the room's.
      onResync: () => refresh(client),
      onStatus: setConnectionStatus,
    })
    return () => socket.close()
  }, [client, roomId])
}
