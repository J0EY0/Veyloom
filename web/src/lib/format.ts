import { getLocale, t } from './i18n'

// Time and number formatting, in the current language. Formatters are
// built per locale on first use; Intl objects are expensive to construct
// and cheap to reuse.

interface Formats {
  timeOfDay: Intl.DateTimeFormat
  dayAndTime: Intl.DateTimeFormat
  full: Intl.DateTimeFormat
  day: Intl.DateTimeFormat
  count: Intl.NumberFormat
  compactCount: Intl.NumberFormat
}

const cache = new Map<string, Formats>()

function formats(): Formats {
  const locale = getLocale()
  let found = cache.get(locale)
  if (!found) {
    found = {
      timeOfDay: new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', hour12: false }),
      dayAndTime: new Intl.DateTimeFormat(locale, {
        month: locale === 'en' ? 'short' : 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }),
      full: new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }),
      day: new Intl.DateTimeFormat(locale, { month: locale === 'en' ? 'short' : 'long', day: 'numeric' }),
      count: new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }),
      compactCount: new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }),
    }
    cache.set(locale, found)
  }
  return found
}

// formatTime is the short form next to a message: the clock time today,
// the date and clock time otherwise.
export function formatTime(iso: string, now: Date = new Date()): string {
  const date = new Date(iso)
  return sameDay(date, now) ? formats().timeOfDay.format(date) : formats().dayAndTime.format(date)
}

// formatDay is the date without the year or the time: "9月16日", "Sep 16".
export function formatDay(iso: string): string {
  return formats().day.format(new Date(iso))
}

// formatHour is the clock time alone: "14:00".
export function formatHour(iso: string): string {
  return formats().timeOfDay.format(new Date(iso))
}

// browserTimeZone names the time zone the browser is in, for asking the hub
// for hours and days that begin here.
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

// formatFullTime is the tooltip form: unambiguous, with the year.
export function formatFullTime(iso: string): string {
  return formats().full.format(new Date(iso))
}

function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

// formatDuration says how long something took, in whole seconds.
export function formatDuration(startIso: string, endIso: string): string {
  return formatSpan(new Date(endIso).getTime() - new Date(startIso).getTime())
}

// formatSpan says how long a number of milliseconds is, in whole seconds.
export function formatSpan(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000))
  if (seconds < 60) return t('duration.seconds', { n: seconds })
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return rest === 0 ? t('duration.minutes', { n: minutes }) : t('duration.both', { m: minutes, s: rest })
}

// formatLongSpan says a span of days, hours and minutes, to the minute:
// "2 天 4 小时", "3h 20m".
export function formatLongSpan(ms: number): string {
  const minutes = Math.max(0, Math.round(ms / 60_000))
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  const rest = minutes % 60
  return [
    days > 0 ? t('duration.days', { n: days }) : '',
    hours > 0 ? t('duration.hours', { n: hours }) : '',
    rest > 0 || minutes < 60 ? t('duration.minutes', { n: rest }) : '',
  ]
    .filter(Boolean)
    .join(' ')
}

// formatElapsed is a running clock, minutes and seconds: how long a turn
// has been going.
export function formatElapsed(startIso: string, now: number): string {
  const seconds = Math.max(0, Math.floor((now - new Date(startIso).getTime()) / 1000))
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`
}

// formatAgo says how long ago something happened, coarsely: heartbeats
// and the like do not need seconds once they are minutes old.
export function formatAgo(iso: string, now: Date = new Date()): string {
  const seconds = Math.max(0, Math.round((now.getTime() - new Date(iso).getTime()) / 1000))
  if (seconds < 10) return t('ago.now')
  if (seconds < 60) return t('ago.seconds', { n: seconds })
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return t('ago.minutes', { n: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('ago.hours', { n: hours })
  return t('ago.days', { n: Math.floor(hours / 24) })
}

// formatAgoParts is formatAgo split where the eye should land: the amount
// ("12m", "12 分钟", "just now") and what follows it (" ago", "前", or
// nothing). The ago.* phrases all end in ago.suffix except ago.now.
export function formatAgoParts(iso: string, now: Date = new Date()): [amount: string, rest: string] {
  const text = formatAgo(iso, now)
  const suffix = t('ago.suffix')
  return text !== t('ago.now') && text.endsWith(suffix) ? [text.slice(0, text.length - suffix.length), suffix] : [text, '']
}

// formatBytes says how big a file is, in the unit that reads.
export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  const kb = size / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

// formatCount writes a whole number in full, grouped the local way:
// 1,234,567.
export function formatCount(n: number): string {
  return formats().count.format(n)
}

// formatCompactCount writes a number the short way for a glance: 12.3K,
// or 12.3万 in Chinese, which only shortens from ten thousand up.
export function formatCompactCount(n: number): string {
  return formats().compactCount.format(n)
}
