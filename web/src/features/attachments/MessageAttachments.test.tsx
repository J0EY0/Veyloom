import { act, fireEvent, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { attachment } from '@/test/fixtures'
import { stubApi } from '@/test/fetch'
import { intersectAll } from '@/test/intersection'
import { renderWithProviders } from '@/test/render'
import { fit, MessageAttachments } from './MessageAttachments'
import { openViewer } from './viewerStore'

// Highlighting waits on shiki; the code as it is is what matters here.
vi.mock('@/components/ai-elements/code-block', () => ({
  CodeBlock: ({ code, language }: { code: string; language: string }) => <pre data-language={language}>{code}</pre>,
}))

vi.mock('./pdf', async () => {
  const doc = (await import('@/test/pdf')).fakePdf(6)
  return { usePdf: (_url: string, enabled = true) => (enabled ? { data: doc, isError: false } : { data: undefined, isError: false }) }
})

vi.mock('./viewerStore', async (original) => ({ ...(await original<typeof import('./viewerStore')>()), openViewer: vi.fn() }))

afterEach(() => vi.mocked(openViewer).mockClear())

describe('how big a picture is shown', () => {
  it('keeps its shape, no bigger than itself nor 20rem either way', () => {
    expect(fit(1600, 1000)).toEqual({ w: 20, h: 12.5 })
    expect(fit(1000, 1600)).toEqual({ w: 12.5, h: 20 })
    // A small one stays its own size: 160 pixels are 10rem.
    expect(fit(160, 80)).toEqual({ w: 10, h: 5 })
    // A sliver still shows.
    expect(fit(4000, 10)).toEqual({ w: 20, h: 2 })
  })

  it('holds a 16:10 box for a picture whose size is not known', () => {
    expect(fit()).toEqual({ w: 16, h: 10 })
    expect(fit(0, 300)).toEqual({ w: 16, h: 10 })
  })
})

// What a message carries is drawn in the chat (docs/webui.md 4.21).
describe('attachments in the chat', () => {
  it('draws nothing for a message without any', () => {
    renderWithProviders(<MessageAttachments attachments={null} roomId="r1" />)
    expect(screen.queryByRole('group', { name: '附件' })).not.toBeInTheDocument()
  })

  it('shows one picture at its own shape by its smaller copy, and opens it in the viewer', async () => {
    const shot = attachment('p1', 'list-narrow.png', 'image', { width: 1600, height: 1000, thumbnail: true })
    renderWithProviders(<MessageAttachments attachments={[shot]} roomId="r1" threadId="t1" />)
    const picture = await screen.findByRole('button', { name: '预览 list-narrow.png' })
    expect(picture.style.width).toBe('20rem')
    expect(picture.querySelector('img')).toHaveAttribute('src', '/api/v1/attachments/p1/thumbnail')
    await userEvent.click(picture)
    expect(openViewer).toHaveBeenCalledWith({ roomId: 'r1', attachment: shot, threadId: 't1' })
  })

  it('puts pictures and video first, several of them in a grid', async () => {
    const files = [
      attachment('f1', 'spec.zip', 'archive'),
      attachment('p1', 'a.png', 'image'),
      attachment('v1', 'hang.mp4', 'video'),
      attachment('p2', 'b.png', 'image'),
    ]
    renderWithProviders(<MessageAttachments attachments={files} roomId="r1" />)
    const group = await screen.findByRole('group', { name: '附件' })
    const opens = within(group)
      .getAllByRole('button', { name: /^预览/ })
      .map((b) => b.getAttribute('aria-label'))
    expect(opens).toEqual(['预览 a.png', '预览 hang.mp4', '预览 b.png', '预览 spec.zip'])
    const tile = screen.getByRole('button', { name: '预览 hang.mp4' })
    expect(tile.querySelector('video')).toHaveAttribute('src', '/api/v1/attachments/v1')
    expect(screen.getByRole('button', { name: '预览 a.png' }).querySelector('img')).toHaveAttribute('src', '/api/v1/attachments/p1')
  })

  it('plays one video where it was sent, with a way to the viewer', async () => {
    const clip = attachment('v1', 'hang.mp4', 'video')
    renderWithProviders(<MessageAttachments attachments={[clip]} roomId="r1" />)
    expect(await screen.findByLabelText('hang.mp4')).toHaveAttribute('controls')
    await userEvent.click(screen.getByRole('button', { name: '预览 hang.mp4' }))
    expect(openViewer).toHaveBeenCalledWith({ roomId: 'r1', attachment: clip, threadId: undefined })
  })

  it('shows the first lines of a text file, and the rest when asked', async () => {
    const body = Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join('\n') + '\n'
    stubApi({ '/attachments/t1': new Response(body, { status: 206, headers: { 'Content-Range': `bytes 0-${body.length - 1}/${body.length}` } }) })
    renderWithProviders(<MessageAttachments attachments={[attachment('t1', 'tags_test.go', 'text', { size: 699 })]} roomId="r1" />)
    const code = await screen.findByText((_, node) => node?.tagName === 'PRE' && node.textContent?.startsWith('line 1\n') === true)
    expect(code).toHaveAttribute('data-language', 'go')
    expect(code.textContent).toBe(body.split('\n').slice(0, 9).join('\n'))
    expect(screen.getByText('12 行 · 699 B')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '展开其余 3 行' }))
    // The whole file, its last break starting no empty line.
    expect(code.textContent).toBe(body.trimEnd())
    await userEvent.click(screen.getByRole('button', { name: '收起' }))
    expect(code.textContent).not.toContain('line 10')
  })

  it('sends a text file too long for the chat to the viewer', async () => {
    const head = 'a\n'.repeat(20)
    stubApi({ '/attachments/t1': new Response(head, { status: 206, headers: { 'Content-Range': 'bytes 0-39/900000' } }) })
    const log = attachment('t1', 'huge.log', 'text', { size: 900000 })
    renderWithProviders(<MessageAttachments attachments={[log]} roomId="r1" />)
    await userEvent.click(await screen.findByRole('button', { name: '展开' }))
    await userEvent.click(screen.getByRole('button', { name: '查看全文' }))
    expect(openViewer).toHaveBeenCalledWith({ roomId: 'r1', attachment: log, threadId: undefined })
    // How many lines a file has is not known from its start.
    expect(screen.queryByText(/行 ·/)).not.toBeInTheDocument()
  })

  it('draws a Markdown file as a document', async () => {
    stubApi({ '/attachments/n1': new Response('# 标签规则\n\n- 不分大小写\n', { status: 200 }) })
    renderWithProviders(<MessageAttachments attachments={[attachment('n1', 'NOTES.md', 'text')]} roomId="r1" />)
    expect(await screen.findByRole('heading', { name: '标签规则' })).toBeInTheDocument()
    expect(screen.getByRole('listitem')).toHaveTextContent('不分大小写')
  })

  it('shows the first page of a PDF once it comes into view', async () => {
    renderWithProviders(<MessageAttachments attachments={[attachment('d1', '需求 v2.pdf', 'pdf', { size: 7250 })]} roomId="r1" />)
    expect(await screen.findByText('7.1 KB')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: '需求 v2.pdf 的第一页' })).not.toBeInTheDocument()
    act(() => intersectAll())
    expect(await screen.findByRole('img', { name: '需求 v2.pdf 的第一页' })).toBeInTheDocument()
    expect(screen.getByText('6 页 · 7.1 KB')).toBeInTheDocument()
  })

  it('plays sound in place', async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
      this.dispatchEvent(new Event('play'))
      return Promise.resolve()
    })
    const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function (this: HTMLMediaElement) {
      this.dispatchEvent(new Event('pause'))
    })
    const { container } = renderWithProviders(<MessageAttachments attachments={[attachment('s1', '补充.m4a', 'audio')]} roomId="r1" />)
    const audio = container.querySelector('audio')!
    expect(audio).toHaveAttribute('src', '/api/v1/attachments/s1')
    Object.defineProperty(audio, 'duration', { value: 65 })
    fireEvent.loadedMetadata(audio)
    expect(await screen.findByText('0:00 / 1:05')).toBeInTheDocument()

    Object.defineProperty(audio, 'paused', { value: true, configurable: true })
    await userEvent.click(screen.getByRole('button', { name: '播放 补充.m4a' }))
    expect(play).toHaveBeenCalled()
    Object.defineProperty(audio, 'paused', { value: false, configurable: true })
    await userEvent.click(screen.getByRole('button', { name: '暂停 补充.m4a' }))
    expect(pause).toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '播放 补充.m4a' })).toBeInTheDocument()

    fireEvent.change(screen.getByRole('slider', { name: '补充.m4a 的进度' }), { target: { value: '30' } })
    expect(audio.currentTime).toBe(30)
    expect(screen.getByText('0:30 / 1:05')).toBeInTheDocument()
    play.mockRestore()
    pause.mockRestore()
  })

  it('cards any other file, to open or download', async () => {
    const zip = attachment('z1', 'linkkeeper-0.3.0.zip', 'archive', { size: 869 })
    renderWithProviders(<MessageAttachments attachments={[zip]} roomId="r1" />)
    const link = await screen.findByRole('link', { name: '下载 linkkeeper-0.3.0.zip' })
    expect(link).toHaveAttribute('href', '/api/v1/attachments/z1')
    expect(link).toHaveAttribute('download', 'linkkeeper-0.3.0.zip')
    await userEvent.click(screen.getByRole('button', { name: '预览 linkkeeper-0.3.0.zip' }))
    expect(openViewer).toHaveBeenCalledWith({ roomId: 'r1', attachment: zip, threadId: undefined })
  })
})
