import { describe, expect, it } from 'vitest'
import { formatAgo, formatAgoParts, formatElapsed, formatTime } from './format'
import { setLocale } from './i18n'

describe('formatTime', () => {
  const now = new Date('2026-09-14T10:05:00')

  it('shows only the clock for today', () => {
    expect(formatTime('2026-09-14T09:41:00', now)).toBe('09:41')
  })

  it('adds the date for another day', () => {
    const text = formatTime('2026-09-13T15:19:00', now)
    expect(text).toContain('9月13日')
    expect(text).toContain('15:19')
  })
})

describe('formatAgo', () => {
  const now = new Date('2026-09-14T10:05:00Z')
  it('rounds to the coarsest unit that reads', () => {
    expect(formatAgo('2026-09-14T10:04:55Z', now)).toBe('刚刚')
    expect(formatAgo('2026-09-14T10:04:20Z', now)).toBe('40 秒前')
    expect(formatAgo('2026-09-14T09:50:00Z', now)).toBe('15 分钟前')
    expect(formatAgo('2026-09-14T07:05:00Z', now)).toBe('3 小时前')
    expect(formatAgo('2026-09-11T10:05:00Z', now)).toBe('3 天前')
  })
})

describe('formats in English', () => {
  it('uses English words and month names', async () => {
    const { setLocale } = await import('./i18n')
    setLocale('en')
    const now = new Date('2026-09-14T10:05:00')
    expect(formatTime('2026-09-13T15:19:00', now)).toMatch(/Sep 13/)
    expect(formatAgo('2026-09-14T09:50:00Z', new Date('2026-09-14T10:05:00Z'))).toBe('15m ago')
  })
})

describe('formatElapsed', () => {
  it('runs as minutes and seconds, never below zero', () => {
    const start = '2026-09-16T10:00:00Z'
    expect(formatElapsed(start, new Date('2026-09-16T10:00:42Z').getTime())).toBe('00:42')
    expect(formatElapsed(start, new Date('2026-09-16T10:12:05Z').getTime())).toBe('12:05')
    expect(formatElapsed(start, new Date('2026-09-16T09:59:00Z').getTime())).toBe('00:00')
  })
})

describe('formatAgoParts', () => {
  const now = new Date('2026-09-16T12:00:00Z')

  it('picks out the amount from what follows it', () => {
    expect(formatAgoParts('2026-09-16T11:48:00Z', now)).toEqual(['12 分钟', '前'])
    expect(formatAgoParts('2026-09-16T10:30:00Z', now)).toEqual(['1 小时', '前'])
    expect(formatAgoParts('2026-09-16T11:59:58Z', now)).toEqual(['刚刚', ''])
  })

  it('does the same in English', () => {
    setLocale('en')
    try {
      expect(formatAgoParts('2026-09-16T11:48:00Z', now)).toEqual(['12m', ' ago'])
      expect(formatAgoParts('2026-09-16T11:59:58Z', now)).toEqual(['just now', ''])
    } finally {
      setLocale('zh-CN')
    }
  })
})
