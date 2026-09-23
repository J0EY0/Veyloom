import { describe, expect, it } from 'vitest'
import type { WikiPageInfo } from '@/api/types'
import { t } from '@/lib/i18n'
import { reviewOrder, reviewText } from './review'

function info(path: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return { path, type: 'Fact', title: path, tags: [], status: 'stable', tier: 'unverified', modified: '2026-09-21T01:00:00Z', resident: false, ...overrides }
}

describe('review', () => {
  const now = new Date('2026-09-23T08:00:00Z')

  it('says why a page is due to be checked again', () => {
    expect(reviewText(t, info('/a.md'), now)).toBe('')
    expect(
      reviewText(t, info('/a.md', { review: { why: 'changed', file: 'internal/hub/brief.go', changed_at: '2026-09-20T03:00:00Z', topic_number: 12 } }), now),
    ).toBe('internal/hub/brief.go 后来改过（9月20日）')
    expect(reviewText(t, info('/a.md', { checked_at: '2026-03-01T08:00:00Z', review: { why: 'period', every: 180 } }), now)).toBe(
      '206 天没有确认过了，这类页每 180 天复核一次',
    )
    expect(reviewText(t, info('/a.md', { stale_after: '2026-09-01T00:00:00Z', review: { why: 'stale' } }), now)).toBe('过了页面写明的有效期（9月1日）')
  })

  it('orders them as the maintainer checks them: a change first, then resident pages, then the longest unchecked', () => {
    const pages = [
      info('/old.md', { checked_at: '2026-01-01T00:00:00Z', review: { why: 'period', every: 60 } }),
      info('/fine.md'),
      info('/older.md', { checked_at: '2025-01-01T00:00:00Z', review: { why: 'period', every: 60 } }),
      info('/resident.md', { resident: true, checked_at: '2026-05-01T00:00:00Z', review: { why: 'period', every: 30 } }),
      info('/changed.md', { checked_at: '2026-09-01T00:00:00Z', review: { why: 'changed', file: 'go.mod' } }),
    ]
    expect(reviewOrder(pages).map((page) => page.path)).toEqual(['/changed.md', '/resident.md', '/older.md', '/old.md'])
  })
})
