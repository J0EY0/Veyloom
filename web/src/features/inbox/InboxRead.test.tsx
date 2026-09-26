import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { InboxItem } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { message, user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { stubWebSocket } from '@/test/websocket'
import { InboxPage } from './InboxPage'

function mention(id: string, seq: number, read: boolean): InboxItem {
  return {
    ...message(id, seq, { member_id: 'a1', user_id: undefined, mentions: [{ kind: 'user', id: 'u1' }], body: `@alice ${id}` }),
    room_name: 'main',
    project_name: 'Veyloom',
    sender_name: 'Pi Tester',
    read,
  }
}

// What a person read of their inbox (docs/webui.md 4.19).
describe('reading the inbox', () => {
  beforeEach(() => {
    setCurrentUser({ id: 'u1', name: 'alice' })
    stubWebSocket()
  })

  it('marks what is unread, reads a mention as it opens, and reads them all at once', async () => {
    const reads: { message_ids?: string[]; up_to?: number }[] = []
    const read = new Set(['m3'])
    const items = () => [mention('m9', 9, read.has('m9')), mention('m7', 7, read.has('m7')), mention('m3', 3, true)]
    stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/approvals': { approvals: [] },
      '/users/u1/inbox': () => ({ items: items(), unread: items().filter((item) => !item.read).length }),
      '/users/u1/inbox/read': async (req: Request) => {
        const body = (await req.json()) as (typeof reads)[number]
        reads.push(body)
        for (const item of items()) if (body.message_ids?.includes(item.id) || (body.up_to !== undefined && item.seq <= body.up_to)) read.add(item.id)
        return { marked: 1, unread: items().filter((item) => !item.read).length }
      },
    })
    renderWithProviders(<InboxPage />, { route: '/inbox', path: '/inbox' })
    const rows = within(await screen.findByRole('list')).getAllByRole('link')
    expect(within(rows[0]).getByRole('img', { name: '未读' })).toBeInTheDocument()
    expect(within(rows[2]).queryByRole('img', { name: '未读' })).toBeNull()

    await userEvent.click(rows[1])
    await waitFor(() => expect(reads).toEqual([{ message_ids: ['m7'] }]))
    const row = (n: number) => within(screen.getByRole('list')).getAllByRole('link')[n]
    await waitFor(() => expect(within(row(1)).queryByRole('img', { name: '未读' })).toBeNull())

    await userEvent.click(screen.getByRole('button', { name: '全部标为已读' }))
    await waitFor(() => expect(reads[1]).toEqual({ up_to: 9 }))
    await waitFor(() => expect(within(row(0)).queryByRole('img', { name: '未读' })).toBeNull())
    expect(screen.queryByRole('button', { name: '全部标为已读' })).toBeNull()
  })

  it('offers nothing to read when all is read', async () => {
    stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/approvals': { approvals: [] },
      '/users/u1/inbox': { items: [mention('m3', 3, true)], unread: 0 },
    })
    renderWithProviders(<InboxPage />, { route: '/inbox', path: '/inbox' })
    await screen.findByRole('list')
    expect(screen.queryByRole('button', { name: '全部标为已读' })).toBeNull()
  })
})
