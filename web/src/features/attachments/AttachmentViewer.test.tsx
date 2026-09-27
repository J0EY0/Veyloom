import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RoomAttachment } from '@/api/types'
import { roomAttachment } from '@/test/fixtures'
import { stubApi } from '@/test/fetch'
import { intersectAll, leaveAll } from '@/test/intersection'
import { renderWithProviders } from '@/test/render'
import { AttachmentViewer } from './AttachmentViewer'
import { nextZoom } from './ViewerStage'
import { closeViewer, openViewer, type ViewerTarget } from './viewerStore'

vi.mock('@/components/ai-elements/code-block', () => ({
  CodeBlock: ({ code }: { code: string }) => <pre>{code}</pre>,
}))

vi.mock('./pdf', async () => {
  const doc = (await import('@/test/pdf')).fakePdf(3)
  return { usePdf: () => ({ data: doc, isPending: false, isError: false }) }
})

afterEach(() => act(() => closeViewer()))

// The room's attachments as the hub lists them, newest first: a message
// sent a picture and a video, a later one a spreadsheet.
const shot = roomAttachment('a', 'shot.png', 'image', { width: 1600, height: 1000, message_id: 'm1', message_seq: 1 })
const clip = roomAttachment('b', 'hang.mp4', 'video', { message_id: 'm1', message_seq: 1 })
const sheet = roomAttachment('c', '排期.xlsx', 'office', { message_id: 'm2', message_seq: 2, thread_id: 't1', thread_number: 3 })
const newestFirst = [sheet, clip, shot]

function stubRoom(list: RoomAttachment[] = newestFirst) {
  return stubApi({ '/rooms/r1/attachments': { attachments: list, total: list.length } })
}

function open(target: ViewerTarget) {
  const view = renderWithProviders(<AttachmentViewer />, { route: '/rooms/r1' })
  act(() => openViewer(target))
  return view
}

async function showing(name: string, at: string) {
  const dialog = await screen.findByRole('dialog', { name })
  await within(dialog).findByText(at)
  return dialog
}

// The viewer shows an attachment in full and steps through the room's
// (docs/webui.md 4.21).
describe('the attachment viewer', () => {
  it('walks the room from the oldest on the left to the newest on the right, as the chat reads', async () => {
    stubRoom()
    open({ roomId: 'r1', attachment: clip })
    await showing('hang.mp4', '2 / 3')
    const user = userEvent.setup()
    await user.keyboard('{ArrowRight}')
    await showing('排期.xlsx', '3 / 3')
    expect(screen.queryByRole('button', { name: '下一个附件' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '上一个附件' }))
    await showing('hang.mp4', '2 / 3')
    await user.keyboard('{ArrowLeft}')
    await showing('shot.png', '1 / 3')
    expect(screen.queryByRole('button', { name: '上一个附件' })).not.toBeInTheDocument()
  })

  it('walks the attachments tab in its own order', async () => {
    const bySize = [clip, shot, sheet]
    const calls = stubRoom(bySize)
    open({ roomId: 'r1', attachment: shot, filter: { q: '', kinds: [], sender: '', sort: 'size' } })
    await showing('shot.png', '2 / 3')
    expect(calls[0]).toContain('sort=size')
    await userEvent.setup().keyboard('{ArrowRight}')
    await showing('排期.xlsx', '3 / 3')
  })

  it('loads more of the room until it finds the one opened', async () => {
    const older = Array.from({ length: 60 }, (_, i) => roomAttachment(`n${i}`, `n${i}.png`, 'image'))
    stubApi({
      '/rooms/r1/attachments': (req: Request) =>
        new URL(req.url).searchParams.get('offset') === '0' ? { attachments: older, total: 61 } : { attachments: [shot], total: 61 },
    })
    open({ roomId: 'r1', attachment: shot })
    await showing('shot.png', '1 / 61')
  })

  it('asks no more for a page that would not load', async () => {
    const older = Array.from({ length: 60 }, (_, i) => roomAttachment(`n${i}`, `n${i}.png`, 'image'))
    const calls = stubApi({
      '/rooms/r1/attachments': (req: Request) =>
        new URL(req.url).searchParams.get('offset') === '0' ? { attachments: older, total: 61 } : Response.json({ error: 'room gone' }, { status: 404 }),
    })
    open({ roomId: 'r1', attachment: shot })
    await waitFor(() => expect(calls.filter((c) => c.includes('offset=60'))).toHaveLength(1))
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(calls.filter((c) => c.includes('offset=60'))).toHaveLength(1)
  })

  it('tells who sent it, when, and how big it is', async () => {
    stubRoom()
    open({ roomId: 'r1', attachment: shot })
    const dialog = await showing('shot.png', '1 / 3')
    expect(within(dialog).getByText(/^Alice · .+ · 1600 × 1000 · 2.0 KB$/)).toBeInTheDocument()
  })

  it('goes back to the message in the chat, or to its topic', async () => {
    stubRoom()
    const { router } = open({ roomId: 'r1', attachment: shot })
    await showing('shot.png', '1 / 3')
    await userEvent.click(screen.getByRole('button', { name: '在聊天中查看' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.state.location.search).toBe('?message=m1')

    act(() => openViewer({ roomId: 'r1', attachment: sheet }))
    await showing('排期.xlsx', '3 / 3')
    await userEvent.click(screen.getByRole('button', { name: '在聊天中查看' }))
    expect(router.state.location.search).toBe('?thread=t1')
  })

  it('closes on Escape', async () => {
    stubRoom()
    open({ roomId: 'r1', attachment: shot })
    await showing('shot.png', '1 / 3')
    await userEvent.setup().keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('zooms a picture in steps and fits it again', async () => {
    stubRoom()
    open({ roomId: 'r1', attachment: shot })
    const dialog = await showing('shot.png', '1 / 3')
    const zoom = within(dialog).getByRole('group', { name: '缩放' })
    const fitted = within(zoom).getByRole('button', { name: '适应窗口' })
    expect(fitted).toHaveAttribute('aria-pressed', 'true')
    const user = userEvent.setup()
    await user.click(within(zoom).getByRole('button', { name: '放大' }))
    await user.click(within(zoom).getByRole('button', { name: '放大' }))
    expect(within(zoom).getByText('200%')).toBeInTheDocument()
    // 1600 pixels twice over are 3200, or 200rem.
    expect(within(dialog).getByRole('img', { name: 'shot.png' }).style.width).toBe('200rem')
    await user.click(fitted)
    expect(fitted).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByRole('img', { name: 'shot.png' }).style.width).toBe('')
  })

  it('reads a text file whole and copies it', async () => {
    const notes = roomAttachment('t', 'notes.txt', 'text')
    stubApi({
      '/rooms/r1/attachments': { attachments: [notes], total: 1 },
      '/attachments/t': new Response('第一行\n第二行\n', { status: 200 }),
    })
    const user = userEvent.setup()
    open({ roomId: 'r1', attachment: notes })
    const dialog = await showing('notes.txt', '1 / 1')
    expect(await within(dialog).findByText((_, node) => node?.tagName === 'PRE' && node.textContent === '第一行\n第二行')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '复制' }))
    expect(await navigator.clipboard.readText()).toBe('第一行\n第二行\n')
  })

  it('draws the pages of a PDF, each as it comes near', async () => {
    const spec = roomAttachment('d', 'spec.pdf', 'pdf')
    stubApi({ '/rooms/r1/attachments': { attachments: [spec], total: 1 } })
    open({ roomId: 'r1', attachment: spec })
    const dialog = await showing('spec.pdf', '1 / 1')
    expect(await within(dialog).findByRole('img', { name: '第 1 页' })).toBeInTheDocument()
    expect(within(dialog).getByRole('img', { name: '第 2 页' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('img', { name: '第 3 页' })).not.toBeInTheDocument()
    act(() => intersectAll())
    expect(await within(dialog).findByRole('img', { name: '第 3 页' })).toBeInTheDocument()
    // Far from the view, a page lets its drawing go, and draws again when
    // it comes back.
    act(() => leaveAll())
    await waitFor(() => expect(within(dialog).queryByRole('img', { name: /第 \d 页/ })).not.toBeInTheDocument())
    act(() => intersectAll())
    expect(await within(dialog).findAllByRole('img', { name: /第 \d 页/ })).toHaveLength(3)
  })

  it('offers a file it cannot show to download', async () => {
    stubRoom()
    open({ roomId: 'r1', attachment: sheet })
    const dialog = await showing('排期.xlsx', '3 / 3')
    expect(within(dialog).getByText('这种文件在这里看不了，下载后打开。')).toBeInTheDocument()
    const links = within(dialog).getAllByRole('link', { name: /下载/ })
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['/api/v1/attachments/c', '/api/v1/attachments/c'])
  })
})

describe('zooming a picture', () => {
  it('steps from where it is to the next size either way, and stops at the ends', () => {
    expect(nextZoom(1, 1)).toBe(1.5)
    expect(nextZoom(1, -1)).toBe(0.75)
    // Fitted at 78%, a step in is 100% and a step out 75%.
    expect(nextZoom(0.78, 1)).toBe(1)
    expect(nextZoom(0.78, -1)).toBe(0.75)
    expect(nextZoom(4, 1)).toBe(4)
    expect(nextZoom(0.25, -1)).toBe(0.25)
    expect(nextZoom(0.1, 1)).toBe(0.25)
  })
})
