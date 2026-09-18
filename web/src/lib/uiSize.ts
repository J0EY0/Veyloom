import { useSyncExternalStore } from 'react'

// How large the interface draws. It sets the root font size, and every size
// in the UI is in rem, so text and spacing grow together the way browser
// zoom would, while breakpoints (rem in media queries is the browser's own
// 16px) stay put. The size is a percentage of the browser's default, so a
// larger font chosen in the browser still counts. Kept per browser, like
// the theme and the language.

export type UiSize = 'small' | 'default' | 'large'

export const uiSizes: UiSize[] = ['small', 'default', 'large']

const STORAGE_KEY = 'veyloom.uiSize'
// Small is the compact size the interface was drawn at, Linear's 13px
// navigation and 14.5px replies; that read too small as the default
// (2026-09-16), so the default is an eighth larger, where replies land near
// the 16px chat apps use, and large another eighth on top.
const rootFontSize: Record<UiSize, string> = {
  small: '100%',
  default: '112.5%',
  large: '125%',
}

function read(): UiSize {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return uiSizes.includes(raw as UiSize) ? (raw as UiSize) : 'default'
  } catch {
    return 'default'
  }
}

let current: UiSize = read()
const listeners = new Set<() => void>()

export function applyUiSize(size: UiSize = current) {
  document.documentElement.style.fontSize = rootFontSize[size]
}

export function getUiSize(): UiSize {
  return current
}

export function setUiSize(size: UiSize) {
  current = size
  try {
    if (size === 'default') {
      localStorage.removeItem(STORAGE_KEY)
    } else {
      localStorage.setItem(STORAGE_KEY, size)
    }
  } catch {
    // The choice then lasts the session.
  }
  applyUiSize(size)
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useUiSize(): UiSize {
  return useSyncExternalStore(subscribe, getUiSize, getUiSize)
}
