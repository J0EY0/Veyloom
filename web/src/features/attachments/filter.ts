import type { AttachmentFilter } from '@/api/attachments'
import type { AttachmentSort, RoomAttachment } from '@/api/types'
import { formatHour, formatTime } from '@/lib/format'
import { t } from '@/lib/i18n'
import { kindGroups, type KindGroup } from './kinds'

// What the attachments tab shows lives in its address (docs/webui.md
// 4.21), so a reload keeps it and the search can link to it:
// ?q=<words>&kind=media|docs|other&sender=user:<id>|member:<id>&sort=oldest|size|name.
// Newest first and every kind are what an address without them means.

export interface TabFilter {
  q: string
  group: KindGroup
  sender: string
  sort: AttachmentSort
}

const groups: KindGroup[] = ['all', 'media', 'docs', 'other']
export const sorts: AttachmentSort[] = ['newest', 'oldest', 'size', 'name']

export function readFilter(params: URLSearchParams): TabFilter {
  const group = params.get('kind') as KindGroup | null
  const sort = params.get('sort') as AttachmentSort | null
  return {
    q: params.get('q') ?? '',
    group: group && groups.includes(group) ? group : 'all',
    sender: params.get('sender') ?? '',
    sort: sort && sorts.includes(sort) ? sort : 'newest',
  }
}

// writeFilter changes some of the filter in an address, leaving out what
// is the default and keeping what else the address says (an open topic).
export function writeFilter(params: URLSearchParams, change: Partial<TabFilter>): URLSearchParams {
  const next = new URLSearchParams(params)
  const put = (key: string, value: string, fallback: string) => (value === fallback ? next.delete(key) : next.set(key, value))
  if (change.q !== undefined) put('q', change.q.trim(), '')
  if (change.group !== undefined) put('kind', change.group, 'all')
  if (change.sender !== undefined) put('sender', change.sender, '')
  if (change.sort !== undefined) put('sort', change.sort, 'newest')
  return next
}

// queryOf is what the hub is asked for.
export function queryOf(filter: TabFilter): AttachmentFilter {
  return { q: filter.q.trim(), kinds: kindGroups[filter.group], sender: filter.sender, sort: filter.sort }
}

// byDay says whether the list goes in days: when it goes by time.
export function byDay(sort: AttachmentSort): boolean {
  return sort === 'newest' || sort === 'oldest'
}

export interface Day {
  key: string
  // The first attachment's time, for naming the day.
  at: string
  attachments: RoomAttachment[]
}

function dayKey(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

// splitDays cuts a list sorted by time into its days, in the order they
// come.
export function splitDays(list: RoomAttachment[]): Day[] {
  const days: Day[] = []
  for (const attachment of list) {
    const key = dayKey(attachment.created_at)
    const last = days[days.length - 1]
    if (last && last.key === key) last.attachments.push(attachment)
    else days.push({ key, at: attachment.created_at, attachments: [attachment] })
  }
  return days
}

// relativeDay names today and yesterday; any other day has no such name.
export function relativeDay(iso: string, now: Date = new Date()): string | undefined {
  const key = dayKey(iso)
  if (key === dayKey(now.toISOString())) return t('attachments.today')
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  return key === dayKey(yesterday.toISOString()) ? t('attachments.yesterday') : undefined
}

// whenSent is a card's time: the clock time under its day's heading, and
// the day with it otherwise.
export function whenSent(iso: string, inDays: boolean, now: Date = new Date()): string {
  if (inDays) return formatHour(iso)
  const day = relativeDay(iso, now)
  return day ? `${day} ${formatHour(iso)}` : formatTime(iso, now)
}
