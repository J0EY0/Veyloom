import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { setConnectionStatus } from '@/lib/connection'
import { applyTurnEvent, clearLiveTurn } from '@/lib/liveTurns'
import { connectRoomEvents } from '@/lib/roomSocket'
import { applyApproval } from './approvals'
import { applyRoomMessage, applyTurn } from './messages'
import { topicKeys } from './topics'
import { applyRunningTurn } from './turns'
import type { RoomEvent } from './types'

// applyRoomEvent writes one live event into the query cache, which is the
// only store the UI reads (docs/webui.md §5.2).
export function applyRoomEvent(client: QueryClient, event: RoomEvent) {
  switch (event.kind) {
    case 'message':
      applyRoomMessage(client, event.message, event.thread)
      break
    case 'turn_started':
      applyTurn(client, event.turn)
      applyRunningTurn(client, event.turn)
      void client.invalidateQueries({ queryKey: topicKeys.running })
      break
    case 'turn_finished':
      applyTurn(client, event.turn)
      applyRunningTurn(client, event.turn)
      // Its messages have arrived by now; the live copy is stale.
      clearLiveTurn(event.turn.id)
      void client.invalidateQueries({ queryKey: topicKeys.running })
      break
    case 'turn_event':
      applyTurnEvent(event.turn_id, event.turn_event)
      break
    case 'approval_requested':
    case 'approval_decided':
      applyApproval(client, event.approval)
      break
    default:
      break
  }
}

// useRoomEvents keeps the room's live stream flowing into the cache while
// the room is open. Reconnecting refetches everything on screen.
export function useRoomEvents(roomId: string) {
  const client = useQueryClient()
  useEffect(() => {
    if (roomId === '') return
    const socket = connectRoomEvents(roomId, {
      onEvent: (event) => applyRoomEvent(client, event),
      // A reconnect means anything may have changed meanwhile, the server
      // included (a restart, a wiped database): refetch every query on
      // screen, not just the room's.
      onResync: () => {
        void client.invalidateQueries()
      },
      onStatus: setConnectionStatus,
    })
    return () => socket.close()
  }, [client, roomId])
}
