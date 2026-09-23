import type { WikiPageInfo } from '@/api/types'
import { formatDay } from '@/lib/format'
import type { t as translate } from '@/lib/i18n'

type T = typeof translate

const dayMs = 24 * 60 * 60 * 1000

// reviewText is why a page is due to be checked again (docs/design.md
// 5.16), in words; empty for a page that is not.
export function reviewText(t: T, page: WikiPageInfo, now: Date = new Date()): string {
  const review = page.review
  if (!review) return ''
  switch (review.why) {
    case 'changed':
      return t('wiki.review.changed', { file: review.file ?? '', when: review.changed_at ? formatDay(review.changed_at) : '' })
    case 'stale':
      return t('wiki.review.stale', { when: page.stale_after ? formatDay(page.stale_after) : '' })
    default: {
      const days = page.checked_at ? Math.floor((now.getTime() - new Date(page.checked_at).getTime()) / dayMs) : 0
      return t('wiki.review.period', { days, every: review.every ?? 0 })
    }
  }
}

// reviewOrder is the order the maintainer checks the pages due in: those a
// change calls for first, then the resident ones every turn carries, then
// the longest unchecked (as the hub orders them, internal/hub/review.go).
export function reviewOrder(pages: WikiPageInfo[]): WikiPageInfo[] {
  const rank = (page: WikiPageInfo) => (page.review?.why === 'changed' ? 0 : page.resident ? 1 : 2)
  const checked = (page: WikiPageInfo) => (page.checked_at ? new Date(page.checked_at).getTime() : 0)
  return pages.filter((page) => page.review).sort((a, b) => rank(a) - rank(b) || checked(a) - checked(b))
}
