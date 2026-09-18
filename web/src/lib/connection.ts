import { useSyncExternalStore } from 'react'

// The state of the room's live connection, for the one place that shows it.
export type ConnectionStatus = 'connecting' | 'open' | 'reconnecting'

let status: ConnectionStatus = 'connecting'
const listeners = new Set<() => void>()

export function setConnectionStatus(next: ConnectionStatus) {
  if (next === status) return
  status = next
  listeners.forEach((listener) => listener())
}

export function getConnectionStatus(): ConnectionStatus {
  return status
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useConnectionStatus(): ConnectionStatus {
  return useSyncExternalStore(subscribe, getConnectionStatus, getConnectionStatus)
}
