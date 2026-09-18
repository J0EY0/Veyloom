import { useSyncExternalStore } from 'react'

// Whether the command palette is open. A module store, so the search
// button in a page header can open the palette the shell renders.

let open = false
const listeners = new Set<() => void>()

export function setPaletteOpen(next: boolean) {
  if (next === open) return
  open = next
  listeners.forEach((listener) => listener())
}

export function openPalette() {
  setPaletteOpen(true)
}

function get() {
  return open
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function usePaletteOpen(): boolean {
  return useSyncExternalStore(subscribe, get, get)
}
