import { useSyncExternalStore } from 'react'

// The colour scheme: follow the system unless a person pinned one. The
// choice is stored per browser. theme.css keys everything on a `dark`
// class on <html>, which applyTheme keeps in step with the choice and,
// while following the system, with the system.

export type Theme = 'system' | 'light' | 'dark'
export type Scheme = 'light' | 'dark'

const STORAGE_KEY = 'veyloom.theme'
const themes: Theme[] = ['system', 'light', 'dark']

function read(): Theme {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return themes.includes(raw as Theme) ? (raw as Theme) : 'system'
  } catch {
    return 'system'
  }
}

let current: Theme = read()
const listeners = new Set<() => void>()

function systemScheme(): Scheme {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

// resolveScheme says which scheme a choice comes out as right now.
export function resolveScheme(theme: Theme = current): Scheme {
  return theme === 'system' ? systemScheme() : theme
}

export function applyTheme(theme: Theme = current) {
  const root = document.documentElement
  if (theme === 'system') {
    delete root.dataset.theme
  } else {
    root.dataset.theme = theme
  }
  root.classList.toggle('dark', resolveScheme(theme) === 'dark')
}

export function getTheme(): Theme {
  return current
}

export function setTheme(theme: Theme) {
  current = theme
  try {
    if (theme === 'system') {
      localStorage.removeItem(STORAGE_KEY)
    } else {
      localStorage.setItem(STORAGE_KEY, theme)
    }
  } catch {
    // The choice then lasts the session.
  }
  applyTheme(theme)
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, getTheme, getTheme)
}

// useResolvedScheme is for the few components that need the scheme
// itself, such as the toaster.
export function useResolvedScheme(): Scheme {
  useTheme()
  return resolveScheme()
}

// watchSystemTheme re-applies the theme when the system scheme changes,
// so "follow the system" really follows it. Returns a stop function.
export function watchSystemTheme(): () => void {
  if (typeof matchMedia !== 'function') return () => {}
  const query = matchMedia('(prefers-color-scheme: dark)')
  const onChange = () => {
    applyTheme()
    listeners.forEach((listener) => listener())
  }
  query.addEventListener('change', onChange)
  return () => query.removeEventListener('change', onChange)
}
