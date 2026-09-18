import { useSyncExternalStore } from 'react'

// Who the person at this browser is. There is no login yet (docs/webui.md
// §4.8): the choice lives in localStorage and every write sends its id.

export interface CurrentUser {
  id: string
  name: string
}

const STORAGE_KEY = 'veyloom.user'
const VERSION = 1

interface Stored extends CurrentUser {
  v: number
}

function read(): CurrentUser | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const stored = JSON.parse(raw) as Partial<Stored>
    if (stored.v !== VERSION || typeof stored.id !== 'string' || typeof stored.name !== 'string') {
      return null
    }
    return { id: stored.id, name: stored.name }
  } catch {
    return null
  }
}

let current: CurrentUser | null = read()
const listeners = new Set<() => void>()

export function getCurrentUser(): CurrentUser | null {
  return current
}

export function setCurrentUser(user: CurrentUser | null) {
  current = user
  try {
    if (user) {
      const stored: Stored = { v: VERSION, ...user }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  } catch {
    // Storage can be unavailable (private mode); the choice then lasts the session.
  }
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useCurrentUser(): CurrentUser | null {
  return useSyncExternalStore(subscribe, getCurrentUser, getCurrentUser)
}
