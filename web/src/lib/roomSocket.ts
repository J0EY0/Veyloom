import { wsBase } from '@/api/base'
import type { RoomEvent } from '@/api/types'
import type { ConnectionStatus } from './connection'

export interface RoomSocketHandlers {
  onEvent: (event: RoomEvent) => void
  // Called each time the stream opens, the first time too: the page read
  // the room before the stream was listening, or while it was down, and
  // the stream has no cursor, so the caller refetches what events would
  // have kept current (docs/webui.md §5.3).
  onResync: () => void
  onStatus?: (status: ConnectionStatus) => void
}

export interface RoomSocket {
  close: () => void
}

const initialDelay = 1_000
const maxDelay = 30_000

// connectRoomEvents keeps one WebSocket to a room's event stream open,
// reconnecting with exponential backoff until closed. A close with code
// 1008 means the hub dropped a slow subscriber; it is handled like any
// other drop, since the resync after reconnecting covers what was missed.
export function connectRoomEvents(roomId: string, handlers: RoomSocketHandlers): RoomSocket {
  const url = `${wsBase()}/rooms/${roomId}/events`
  let socket: WebSocket | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let attempt = 0
  let everOpened = false
  let closed = false

  function open() {
    if (closed) return
    handlers.onStatus?.(everOpened ? 'reconnecting' : 'connecting')
    socket = new WebSocket(url)
    socket.onopen = () => {
      attempt = 0
      everOpened = true
      handlers.onResync()
      handlers.onStatus?.('open')
    }
    socket.onmessage = (frame: MessageEvent<string>) => {
      let event: RoomEvent
      try {
        event = JSON.parse(frame.data) as RoomEvent
      } catch {
        return
      }
      handlers.onEvent(event)
    }
    socket.onclose = () => {
      socket = undefined
      if (closed) return
      handlers.onStatus?.('reconnecting')
      const delay = Math.min(initialDelay * 2 ** attempt, maxDelay)
      attempt++
      timer = setTimeout(open, delay)
    }
    socket.onerror = () => {
      // onclose follows; nothing to do here.
    }
  }

  open()
  return {
    close() {
      closed = true
      if (timer) clearTimeout(timer)
      socket?.close()
    },
  }
}
