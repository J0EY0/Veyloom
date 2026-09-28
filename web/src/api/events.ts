import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { setConnectionStatus } from '@/lib/connection'
import { applyTurnEvent, clearLiveTurn } from '@/lib/liveTurns'
import { connectRoomEvents } from '@/lib/roomSocket'
import { addresseeKeys, refreshAddressees } from './addressee'
import { applyApproval } from './approvals'
import { refreshBranches } from './branches'
import { attachmentKeys } from './attachments'
import { applyRoomMessage, applyTurn } from './messages'
import { topicKeys } from './topics'
import { applyQuiet, applyRunningTurn } from './turns'
import type { RoomEvent } from './types'
import { projectKeys } from './projects'
import { refresh } from './refresh'
import { invalidateUpkeep } from './upkeep'
import { refreshRelayHolds } from './relays'
import { applyReminder } from './reminders'
import { applyDraft } from './drafts'
import { invalidateWiki } from './wiki'
import { workKeys } from './work'

// The events that move neither the task board nor the pieces of work: what
// a draft or a reminder did there comes as a note of its own.
const quiet = new Set<RoomEvent['kind']>(['turn_event', 'turn_quiet', 'wiki_changed', 'inbox_read', 'reminder', 'draft'])

// applyRoomEvent writes one live event into the query cache, which is the
// only store the UI reads (docs/webui.md §5.2).
export function applyRoomEvent(client: QueryClient, event: RoomEvent) {
  // The task board and the pieces of work move with the room's turns, its
  // requests, and the notes of merges and resets.
  if (!quiet.has(event.kind)) {
    if (event.kind !== 'message' || event.message.sender_kind === 'system') {
      refresh(client, { queryKey: workKeys.tasks(event.room_id) })
      refresh(client, { queryKey: workKeys.all })
    }
  }
  switch (event.kind) {
    case 'message':
      applyRoomMessage(client, event.message, event.thread)
      // Files it brought join the attachments tab and the viewer's walk.
      if (event.message.attachments?.length) refresh(client, { queryKey: attachmentKeys.room(event.room_id) })
      // A member's word in a topic may make it the one a message there
      // without an @ goes to (docs/design.md 4.2).
      if (event.message.sender_kind === 'agent' && event.message.thread_id) refreshAddressee(client, event.room_id, event.message.thread_id)
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
      refreshAddressee(client, event.room_id, event.turn.thread_id)
      invalidateUpkeep(client)
      refreshBranches(client)
      break
    case 'turn_finished':
      applyTurn(client, event.turn, event.work)
      applyRunningTurn(client, event.turn)
      // Its messages have arrived by now; the live copy is stale.
      clearLiveTurn(event.turn.id)
      refresh(client, { queryKey: topicKeys.running })
      refreshAddressee(client, event.room_id, event.turn.thread_id)
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
    case 'turn_quiet':
      applyQuiet(client, event.room_id, event.turn_id, event.quiet_since)
      break
    case 'approval_requested':
    case 'approval_decided':
      applyApproval(client, event.approval)
      break
    case 'wiki_changed':
      invalidateWiki(client, event.scope === 'library' ? '' : (event.project_id ?? ''))
      break
    case 'reminder':
      applyReminder(client, event.reminder)
      break
    case 'draft':
      applyDraft(client, event.draft)
      break
    default:
      break
  }
}

// refreshAddressee reads again where a message without an @ in the topic
// would go: who runs there, and so takes it, changes with every turn that
// starts or ends there, and who talks or spoke last with what the members
// say there (docs/design.md 4.2). A topic nobody has open reads nothing.
function refreshAddressee(client: QueryClient, roomId: string, threadId: string) {
  refreshAddressees(client, addresseeKeys.at(roomId, threadId))
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
