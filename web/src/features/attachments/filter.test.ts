import { describe, expect, it } from 'vitest'
import { roomAttachment } from '@/test/fixtures'
import { byDay, queryOf, readFilter, relativeDay, splitDays, whenSent, writeFilter } from './filter'

// The attachments tab keeps what it shows in its address (docs/webui.md
// 4.21).
describe("the attachments tab's address", () => {
  it('reads the words, the kinds, the sender and the order, and the defaults when they are missing or wrong', () => {
    expect(readFilter(new URLSearchParams('q=spec&kind=docs&sender=member:m1&sort=size'))).toEqual({
      q: 'spec',
      group: 'docs',
      sender: 'member:m1',
      sort: 'size',
    })
    expect(readFilter(new URLSearchParams('kind=photos&sort=colour'))).toEqual({ q: '', group: 'all', sender: '', sort: 'newest' })
  })

  it('writes a change and leaves out the defaults, keeping the rest of the address', () => {
    const params = new URLSearchParams('thread=t1&sort=size&q=old')
    expect(writeFilter(params, { q: '  标签 需求 ', group: 'media' }).toString()).toBe('thread=t1&sort=size&q=%E6%A0%87%E7%AD%BE+%E9%9C%80%E6%B1%82&kind=media')
    expect(writeFilter(params, { q: ' ', sort: 'newest', group: 'all', sender: '' }).toString()).toBe('thread=t1')
  })

  it('asks the hub for the kinds a group has', () => {
    expect(queryOf({ q: ' spec ', group: 'other', sender: '', sort: 'oldest' })).toEqual({
      q: 'spec',
      kinds: ['audio', 'archive', 'other'],
      sender: '',
      sort: 'oldest',
    })
    expect(queryOf({ q: '', group: 'all', sender: 'user:u1', sort: 'newest' }).kinds).toEqual([])
  })
})

describe('days', () => {
  const now = new Date(2026, 8, 26, 16, 0)
  const at = (day: number, hour: number) => new Date(2026, 8, day, hour, 5).toISOString()

  it('go by time only', () => {
    expect(byDay('newest')).toBe(true)
    expect(byDay('oldest')).toBe(true)
    expect(byDay('size')).toBe(false)
    expect(byDay('name')).toBe(false)
  })

  it('cut a list sorted by time where the day changes', () => {
    const list = [
      roomAttachment('a', 'a.png', 'image', { created_at: at(26, 15) }),
      roomAttachment('b', 'b.png', 'image', { created_at: at(26, 9) }),
      roomAttachment('c', 'c.pdf', 'pdf', { created_at: at(24, 23) }),
    ]
    expect(splitDays(list).map((day) => day.attachments.map((a) => a.id))).toEqual([['a', 'b'], ['c']])
  })

  it('name today and yesterday, and say the day of a card only where no heading does', () => {
    expect(relativeDay(at(26, 1), now)).toBe('今天')
    expect(relativeDay(at(25, 23), now)).toBe('昨天')
    expect(relativeDay(at(24, 12), now)).toBeUndefined()
    expect(whenSent(at(24, 12), true, now)).toBe('12:05')
    expect(whenSent(at(26, 12), false, now)).toBe('今天 12:05')
    expect(whenSent(at(25, 12), false, now)).toBe('昨天 12:05')
    expect(whenSent(at(24, 12), false, now)).toBe('9月24日 12:05')
  })
})
