import { describe, expect, it } from 'vitest'
import type { Usage, UsagePoint } from '@/api/types'
import { anchorOf, cacheShare, changedAt, cumulative, formatSpanTick, peakOf, shareOf, spanTicks } from './usage'

function point(tokens: number, duration_ms = 0): UsagePoint {
  return { at: '2026-09-26T10:00:00Z', tokens, output: 0, duration_ms, turns: 1 }
}

describe('cacheShare', () => {
  it('is what the cache gave of all the input', () => {
    const usage = { total: { input_tokens: 100, cache_read_tokens: 300, cache_write_tokens: 100, output_tokens: 50 } } as Usage
    expect(cacheShare(usage)).toBe(0.6)
    expect(cacheShare({ total: { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 } } as Usage)).toBe(0)
  })
})

describe('cumulative', () => {
  it('adds the points up as they go', () => {
    expect(cumulative([point(5), point(0), point(10)])).toEqual([5, 5, 15])
  })
})

describe('peakOf', () => {
  it('is the first point that spent the most, or took the longest', () => {
    const points = [point(5, 900), point(20, 100), point(20, 300)]
    expect(peakOf(points, 'tokens')).toBe(1)
    expect(peakOf(points, 'duration_ms')).toBe(0)
  })

  it('is none when every point is nothing', () => {
    expect(peakOf([point(0), point(0)], 'tokens')).toBe(-1)
    expect(peakOf([], 'tokens')).toBe(-1)
  })
})

describe('shareOf', () => {
  it('is a part of a whole, and nothing of nothing', () => {
    expect(shareOf(1, 4)).toBe(0.25)
    expect(shareOf(3, 0)).toBe(0)
  })
})

describe('spanTicks', () => {
  it('goes up in whole seconds, minutes or hours, four at most above nought', () => {
    expect(spanTicks(536_005)).toEqual([0, 300_000, 600_000])
    expect(spanTicks(42_000)).toEqual([0, 15_000, 30_000, 45_000])
    expect(spanTicks(3 * 3_600_000)).toEqual([0, 3_600_000, 7_200_000, 10_800_000])
    // Nothing still has an axis.
    expect(spanTicks(0)).toEqual([0, 1000])
  })
})

describe('formatSpanTick', () => {
  it('writes nought bare, and whole hours as hours', () => {
    expect(formatSpanTick(0)).toBe('0')
    expect(formatSpanTick(30_000)).toBe('30 秒')
    expect(formatSpanTick(300_000)).toBe('5 分')
    expect(formatSpanTick(7_200_000)).toBe('2 小时')
  })
})

describe('changedAt', () => {
  it('keeps the points whose label differs from the one before', () => {
    expect(changedAt(['10:01', '10:01', '10:02', '10:01'], (label) => label)).toEqual(['10:01', '10:02', '10:01'])
  })
})

describe('anchorOf', () => {
  it('lines a label up from the edge near either end of a chart', () => {
    expect(anchorOf(0, 30)).toBe('start')
    expect(anchorOf(15, 30)).toBe('middle')
    expect(anchorOf(29, 30)).toBe('end')
    expect(anchorOf(1, 2)).toBe('middle')
  })
})
