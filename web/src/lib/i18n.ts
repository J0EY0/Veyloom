import { useSyncExternalStore } from 'react'
import { en } from '@/i18n/en'
import { zhCN, type MessageKey } from '@/i18n/zh-CN'

// Every string the UI shows lives in src/i18n, one file per language; a
// component asks t() for a key. The locale follows the browser unless a
// person picked one in the user menu.

export type Locale = 'zh-CN' | 'en'

export const locales: Locale[] = ['zh-CN', 'en']

const STORAGE_KEY = 'veyloom.locale'
const catalogs: Record<Locale, Record<MessageKey, string>> = { 'zh-CN': zhCN, en }

function detect(): Locale {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'zh-CN' || stored === 'en') return stored
  } catch {
    // No storage: fall through to the browser's language.
  }
  const language = typeof navigator === 'undefined' ? 'zh' : navigator.language
  return language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

let current: Locale = detect()
const listeners = new Set<() => void>()

export function getLocale(): Locale {
  return current
}

// applyLocale tells the document which language it is in.
export function applyLocale(locale: Locale = current) {
  if (typeof document !== 'undefined') document.documentElement.lang = locale
}

export function setLocale(locale: Locale) {
  current = locale
  try {
    localStorage.setItem(STORAGE_KEY, locale)
  } catch {
    // The choice then lasts the session.
  }
  applyLocale(locale)
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, getLocale)
}

export type Params = Record<string, string | number>

// t looks a key up in the current language, falling back to Chinese, and
// fills {name} placeholders from params. A language with plurals writes
// {n?one|many}, chosen by the numeric param n.
export function t(key: MessageKey, params?: Params): string {
  const template = catalogs[current][key] ?? zhCN[key]
  if (!params) return template
  return template
    .replace(/\{(\w+)\?([^|}]*)\|([^}]*)\}/g, (match, name: string, one: string, many: string) =>
      name in params ? (Number(params[name]) === 1 ? one : many) : match,
    )
    .replace(/\{(\w+)\}/g, (match, name: string) => (name in params ? String(params[name]) : match))
}

// useT is t for components: it subscribes, so a language change re-renders.
export function useT(): typeof t {
  useLocale()
  return t
}
