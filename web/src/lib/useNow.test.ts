import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useNow } from './useNow'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

// Standing still, it keeps the time it had; set going, it tells the time
// at once rather than at its first tick, then as it ticks.
it('tells the time at once when set going', () => {
  vi.setSystemTime(Date.parse('2026-09-27T12:00:00Z'))
  const { result, rerender } = renderHook(({ on }) => useNow(on, 30_000), { initialProps: { on: false } })
  const mounted = result.current
  vi.setSystemTime(Date.parse('2026-09-27T12:10:00Z'))
  act(() => vi.advanceTimersByTime(60_000))
  expect(result.current).toBe(mounted)

  rerender({ on: true })
  act(() => vi.advanceTimersByTime(1))
  expect(Math.abs(result.current - Date.parse('2026-09-27T12:11:00Z'))).toBeLessThan(1000)
  act(() => vi.advanceTimersByTime(30_000))
  expect(Math.abs(result.current - Date.parse('2026-09-27T12:11:30Z'))).toBeLessThan(1000)
})
