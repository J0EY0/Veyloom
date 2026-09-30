import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RoomAttachment } from '@/api/types'
import { roomAttachment, user } from '@/test/fixtures'
import { stubApi } from '@/test/fetch'
import { intersectAll } from '@/test/intersection'
import { renderWithProviders } from '@/test/render'
import { AttachmentsView } from './AttachmentsView'
import { openViewer } from './viewerStore'

vi.mock('./viewerStore', async (original) => ({ ...(await original<typeof import('./viewerStore')>()), openViewer: vi.fn() }))

afterEach(() => vi.mocked(openViewer).mockClear())

const today = new Date()
today.setHours(10, 30, 0, 0)
const longAgo = '2026-01-05T02:00:00Z'

// A room's attachments, the newest first: two today, one in January.
const shot = roomAttachment('a', 'shot.png', 'image', { created_at: today.toISOString(), size: 3000 })
const notes = roomAttachment('b', 'notes.txt', 'text', {
  created_at: today.toISOString(),
  sender_kind: 'agent',
  member_id: 'm1',
  user_id: undefined,
  sender_name: 'Coder',
  thread_id: 't1',
})
const old = roomAttachment('c', 'plan.xlsx', 'office', { created_at: longAgo, size: 1024 })

function stubRoom(list: RoomAttachment[] | ((req: Request) => { attachments: RoomAttachment[]; total: number }) = [shot, notes, old]) {
  return stubApi({
    '/users': { users: [user('u1', 'Alice')] },
    '/rooms/r1/members': { members: [{ id: 'm1', room_id: 'r1', agent_id: 'ag1', display_name: 'Coder', enabled: true }] },
    '/rooms/r1/attachments': typeof list === 'function' ? list : { attachments: list, total: list.length },
  })
}

function show(route = '/rooms/r1/attachments') {
  return renderWithProviders(<AttachmentsView roomId="r1" />, { route })
}

function asked(calls: string[]) {
  return calls.filter((c) => c.startsWith('GET /rooms/r1/attachments?')).map((c) => new URLSearchParams(c.slice(c.indexOf('?') + 1)))
}

// The chat's Attachments tab (docs/webui.md 4.21).
describe('the attachments tab', () => {
  it('shows every attachment as a card, in days, the newest first', async () => {
    stubRoom()
    show()
    const todays = await screen.findByRole('region', { name: '今天' })
    expect(within(todays).getByText('2 个附件')).toBeInTheDocument()
    expect(
      within(todays)
        .getAllByRole('article')
        .map((card) => card.textContent),
    ).toEqual(['shot.pngAlice · 10:30 · 2.9 KB', 'TXTnotes.txtCoder · 10:30 · 2.0 KB'])
    const january = screen.getByRole('region', { name: '1月5日' })
    expect(within(january).getByText('plan.xlsx')).toBeInTheDocument()
    // Days count themselves; the bar does not.
    expect(screen.queryByText('3 个附件')).not.toBeInTheDocument()
  })

  it('narrows by kind, sender and words, and sorts', async () => {
    const calls = stubRoom()
    const { router } = show()
    const user = userEvent.setup()
    await screen.findByText('shot.png')

    await user.click(screen.getByRole('tab', { name: '图片和视频' }))
    await waitFor(() => expect(asked(calls).at(-1)?.get('kind')).toBe('image,video'))
    expect(router.state.location.search).toBe('?kind=media')

    await user.click(screen.getByRole('button', { name: '发送人: 所有人' }))
    await user.click(await screen.findByRole('menuitemradio', { name: 'Coder' }))
    await waitFor(() => expect(asked(calls).at(-1)?.get('sender')).toBe('member:m1'))

    await user.type(screen.getByRole('searchbox', { name: '搜索附件' }), '标签 需求')
    await waitFor(() => expect(asked(calls).at(-1)?.get('q')).toBe('标签 需求'))
    // Typing rested once: one request for the words, not one a key.
    expect(asked(calls).filter((q) => q.has('q'))).toHaveLength(1)
    expect(screen.getByRole('searchbox', { name: '搜索附件' })).toHaveValue('标签 需求')

    await user.click(screen.getByRole('button', { name: '排序: 最新在前' }))
    await user.click(await screen.findByRole('menuitemradio', { name: '按大小' }))
    await waitFor(() => expect(asked(calls).at(-1)?.get('sort')).toBe('size'))
    // Not in days: each card says its day, and the bar counts them.
    expect(await screen.findByText('3 个附件')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '今天' })).not.toBeInTheDocument()
    expect(screen.getByText('Alice · 今天 10:30 · 2.9 KB')).toBeInTheDocument()
    expect(new URLSearchParams(router.state.location.search).get('sort')).toBe('size')
  })

  it('opens a card in the viewer, which walks the cards as the tab has them', async () => {
    stubRoom()
    show('/rooms/r1/attachments?sort=name')
    await userEvent.click(await screen.findByRole('button', { name: '预览 notes.txt' }))
    expect(openViewer).toHaveBeenCalledWith({ roomId: 'r1', attachment: notes, threadId: 't1', filter: { q: '', kinds: [], sender: '', sort: 'name' } })
  })

  it('picks cards and downloads them as one zip, or one file as itself', async () => {
    stubRoom()
    show()
    const user = userEvent.setup()
    await screen.findByText('shot.png')
    await user.click(screen.getByRole('button', { name: '选择' }))
    expect(screen.getByRole('button', { name: '选择' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: '选择 shot.png' }))
    expect(screen.getByRole('button', { name: '选择 shot.png' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('status')).toHaveTextContent('已选 1 个 · 2.9 KB')
    const one = screen.getByRole('link', { name: '下载' })
    expect(one).toHaveAttribute('href', '/api/v1/attachments/a')
    expect(one).toHaveAttribute('download', 'shot.png')

    await user.click(screen.getByRole('button', { name: '选择 plan.xlsx' }))
    expect(screen.getByRole('status')).toHaveTextContent('已选 2 个 · 3.9 KB')
    expect(screen.getByRole('link', { name: '打包下载' })).toHaveAttribute('href', '/api/v1/rooms/r1/attachments/archive?ids=a,c')
    expect(openViewer).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '全选' }))
    expect(screen.getByRole('status')).toHaveTextContent('已选 3 个')
    await user.click(screen.getByRole('button', { name: '取消全选' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '选择 notes.txt' }))
    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选择' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: '预览 shot.png' })).toBeInTheDocument()
  })

  it('keeps only the line, nothing to search or pick, when the room has none', async () => {
    stubRoom([])
    show()
    expect(await screen.findByText('群里还没有附件')).toBeInTheDocument()
    // docs/webui.md §0: a page that is only its empty state drops the
    // search and the filters.
    expect(screen.queryByRole('searchbox', { name: '搜索附件' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: '文档' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择' })).not.toBeInTheDocument()
  })

  it('says none match when a filter is on', async () => {
    stubRoom([])
    show('/rooms/r1/attachments?q=zzz')
    expect(await screen.findByText('没有符合条件的附件')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索附件' })).toHaveValue('zzz')
  })

  it('loads the next page as the end of the cards comes near', async () => {
    const first = Array.from({ length: 60 }, (_, i) => roomAttachment(`p${i}`, `p${i}.png`, 'image', { created_at: longAgo }))
    const calls = stubRoom((req) =>
      new URL(req.url).searchParams.get('offset') === '0' ? { attachments: first, total: 61 } : { attachments: [old], total: 61 },
    )
    show()
    await screen.findByText('p59.png')
    // The day may go on over the page: it is not counted yet.
    expect(screen.queryByText('60 个附件')).not.toBeInTheDocument()
    act(() => intersectAll())
    expect(await screen.findByText('plan.xlsx')).toBeInTheDocument()
    expect(asked(calls).at(-1)?.get('offset')).toBe('60')
    expect(screen.getByText('61 个附件')).toBeInTheDocument()
  })
})
