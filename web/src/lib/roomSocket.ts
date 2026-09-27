import { wsBase } from '@/api/base'
import type { RoomEvent } from '@/api/types'
import type { ConnectionStatus } from './connection'

export interface EventSocketHandlers {
  onEvent: (event: RoomEvent) => void
  // Called each time the stream opens, the first time too: the page read
  // the room before the stream was listening, or while it was down, and
  // the stream has no cursor, so the caller refetches what events would
  // have kept current (docs/webui.md §5.3).
  onResync: () => void
  onStatus?: (status: ConnectionStatus) => void
}

export interface EventSocket {
  close: () => void
}

const initialDelay = 1_000
const maxDelay = 30_000
// heartbeatGap is how long a stream may say nothing before it is taken for
// dead: the hub sends a heartbeat every 25 seconds when nothing else
// happens (docs/design.md 5.23.9).
const heartbeatGap = 60_000

// connectRoomEvents keeps one WebSocket to a room's event stream open.
export function connectRoomEvents(roomId: string, handlers: EventSocketHandlers): EventSocket {
  return connectEvents(`/rooms/${roomId}/events`, handlers)
}

// connectEvents keeps one WebSocket to an event stream of the API open,
// reconnecting with exponential backoff until closed. A close with code
// 1008 means the hub dropped a slow subscriber; it is handled like any
// other drop, since the resync after reconnecting covers what was missed.
// So is a stream that went silent, as one does that died without closing,
// under a laptop that slept or a network that changed: no frame, not even
// a heartbeat, for heartbeatGap.
export function connectEvents(path: string, handlers: EventSocketHandlers): EventSocket {
  const url = `${wsBase()}${path}`
  let socket: WebSocket | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let watchdog: ReturnType<typeof setTimeout> | undefined
  let attempt = 0
  let everOpened = false
  let closed = false

  // alive notes a frame from the hub; heartbeatGap without one, the stream
  // is dropped for dead.
  function alive() {
    if (watchdog) clearTimeout(watchdog)
    watchdog = setTimeout(silent, heartbeatGap)
  }

  // silent drops a stream that stopped saying anything, as if it had
  // closed: a socket whose peer is gone may not tell for a long while.
  function silent() {
    const dead = socket
    if (!dead) return
    dead.onopen = dead.onmessage = dead.onclose = dead.onerror = null
    dead.close()
    dropped()
  }

  // dropped waits a while, longer each time in a row, and opens the stream
  // again.
  function dropped() {
    socket = undefined
    if (watchdog) clearTimeout(watchdog)
    if (closed) return
    handlers.onStatus?.('reconnecting')
    const delay = Math.min(initialDelay * 2 ** attempt, maxDelay)
    attempt++
    timer = setTimeout(open, delay)
  }

  function open() {
    if (closed) return
    handlers.onStatus?.(everOpened ? 'reconnecting' : 'connecting')
    socket = new WebSocket(url)
    socket.onopen = () => {
      attempt = 0
      everOpened = true
      alive()
      handlers.onResync()
      handlers.onStatus?.('open')
    }
    socket.onmessage = (frame: MessageEvent<string>) => {
      alive()
      let event: RoomEvent | { kind: 'heartbeat' }
      try {
        event = JSON.parse(frame.data) as RoomEvent | { kind: 'heartbeat' }
      } catch {
        return
      }
      // Only for alive: nothing to write into the cache.
      if (event.kind === 'heartbeat') return
      handlers.onEvent(event)
    }
    socket.onclose = dropped
    socket.onerror = () => {
      // onclose follows; nothing to do here.
    }
  }

  open()
  return {
    close() {
      closed = true
      if (timer) clearTimeout(timer)
      if (watchdog) clearTimeout(watchdog)
      socket?.close()
    },
  }
}
