import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { message } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { Composer } from './Composer'

describe('Composer', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('is disabled until a user is chosen', () => {
    setCurrentUser(null)
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    expect(screen.getByLabelText('消息')).toBeDisabled()
    expect(screen.getByPlaceholderText('正在确认登录状态…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送' })).toBeDisabled()
  })

  it('sends on Enter and clears the box', async () => {
    let posted: unknown
    stubApi({
      '/rooms/r1/messages': async (req) => {
        posted = await req.json()
        return Response.json({ message: message('m1', 1, { body: 'hello' }) }, { status: 201 })
      },
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')

    await userEvent.type(box, 'hello{Enter}')
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', body: 'hello', mentions: [] }))
    await waitFor(() => expect(box).toHaveValue(''))
  })

  it('waits on Enter while the last message is still going out, keeping what was typed', async () => {
    let release = () => {}
    let posts = 0
    stubApi({
      '/rooms/r1/messages': async () => {
        posts++
        await new Promise<void>((resolve) => {
          release = resolve
        })
        return Response.json({ message: message('m1', 1, { body: 'first' }) }, { status: 201 })
      },
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText<HTMLTextAreaElement>('消息')
    await userEvent.type(box, 'first{Enter}')
    await waitFor(() => expect(posts).toBe(1))

    // Enter again while it goes out: nothing is sent, and nothing lost.
    // (user-event keeps its own idea of the box across the form's reset,
    // so what it typed is taken as it stands.)
    await userEvent.type(box, 'second')
    const typed = box.value
    expect(typed).toContain('second')
    await userEvent.keyboard('{Enter}')
    expect(box).toHaveValue(typed)
    release()
    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeEnabled())
    expect(box).toHaveValue(typed)
    expect(posts).toBe(1)
  })

  it('keeps Shift+Enter as a line break', async () => {
    const calls = stubApi({ '/users': { users: [] }, '/rooms/r1/members': { members: [] } })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')

    await userEvent.type(box, 'a{Shift>}{Enter}{/Shift}b')
    expect(box).toHaveValue('a\nb')
    expect(calls.filter((call) => call.startsWith('POST'))).toEqual([])
  })

  it('keeps what was typed and not sent for when the box comes back, apart for each chat', async () => {
    stubApi({ '/users': { users: [] }, '/rooms/r1/members': { members: [] }, '/rooms/r2/members': { members: [] } })
    const first = renderWithProviders(<Composer roomId="r1" roomName="main" />)
    await userEvent.type(screen.getByLabelText('消息'), 'half a thought')
    first.unmount()

    // Off to another tab of the chat and back: the words are still there.
    const again = renderWithProviders(<Composer roomId="r1" roomName="main" />)
    expect(screen.getByLabelText('消息')).toHaveValue('half a thought')
    again.unmount()

    // Another chat has its own box.
    renderWithProviders(<Composer roomId="r2" roomName="other" />)
    expect(screen.getByLabelText('消息')).toHaveValue('')
  })

  it('forgets the draft once it is sent', async () => {
    stubApi({ '/rooms/r1/messages': Response.json({ message: message('m1', 1, { body: 'hello' }) }, { status: 201 }) })
    const first = renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')
    await userEvent.type(box, 'hello{Enter}')
    await waitFor(() => expect(box).toHaveValue(''))
    first.unmount()

    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    expect(screen.getByLabelText('消息')).toHaveValue('')
  })

  it('shows the server error and keeps the text', async () => {
    stubApi({ '/rooms/r1/messages': Response.json({ error: 'user u1: not found' }, { status: 404 }) })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')

    await userEvent.type(box, 'hello{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('内容不存在，可能已被删除。')
    expect(box).toHaveValue('hello')
  })

  it('says whom a message without an @ goes to, and asks for an @ where it would reach nobody', async () => {
    const asked: string[] = []
    let to: object = { member_id: 'a1', reason: 'leader' }
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Lead', enabled: true }] },
      '/rooms/r1/addressee': (req: Request) => (asked.push(new URL(req.url).search), to),
    })
    const room = renderWithProviders(<Composer roomId="r1" roomName="main" />)
    expect(await screen.findByText('不带 @ 时交给组长 Lead 分派')).toBeInTheDocument()
    expect(room.container.querySelector('kbd')).toBeNull()
    room.unmount()

    to = { reason: 'none' }
    const topic = renderWithProviders(<Composer roomId="r1" roomName="main" threadId="t1" />)
    expect(await screen.findByText('输入 @ 提及成员')).toBeInTheDocument()
    expect(topic.container.querySelector('kbd')).toHaveTextContent('@')
    expect(asked).toEqual(['', '?thread_id=t1'])
  })
})

describe('Composer mentions', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('offers enabled agents after @ and sends the pick as a structured mention', async () => {
    let posted: unknown
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'Codex Implementer', enabled: true },
          { id: 'a2', display_name: 'Pi Tester', enabled: false },
        ],
      },
      '/rooms/r1/messages': async (req) => {
        posted = await req.json()
        return Response.json({ message: message('m1', 1, { body: '@Codex Implementer 补测试' }) }, { status: 201 })
      },
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')
    await waitFor(() => expect(screen.getByText('输入 @ 提及成员')).toBeInTheDocument())

    await userEvent.type(box, '@Co')
    const list = await screen.findByRole('listbox', { name: '提及成员' })
    expect(list).toHaveTextContent('Codex Implementer')
    expect(list).not.toHaveTextContent('Pi Tester')

    await userEvent.keyboard('{Enter}')
    expect(box).toHaveValue('@Codex Implementer ')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    await userEvent.type(box, '补测试{Enter}')
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', body: '@Codex Implementer 补测试', mentions: [{ kind: 'agent', id: 'a1' }] }))
  })
})

describe('Composer take-over', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('accepts text handed to it by a take-over button', async () => {
    stubApi({ '/users': { users: [] }, '/rooms/r1/members': { members: [] } })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')
    await userEvent.type(box, 'hi')
    const { insertIntoComposer } = await import('@/lib/composer')
    expect(insertIntoComposer('room', '@Codex Implementer ')).toBe(true)
    expect(box).toHaveValue('hi @Codex Implementer ')
    expect(box).toHaveFocus()
  })
})

describe('Composer attachments', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('uploads picked files first and posts their ids with the message', async () => {
    let posted: unknown
    let uploaded = ''
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/attachments': async (_req, form) => {
        const file = form?.get('file') as File
        uploaded = `${file.name} ${await file.text()}`
        return Response.json(
          { attachment: { id: 'at1', room_id: 'r1', filename: file.name, media_type: 'text/plain', size: 5, created_at: '2026-09-14T02:00:00Z' } },
          { status: 201 },
        )
      },
      '/rooms/r1/messages': async (req) => {
        posted = await req.json()
        return Response.json({ message: message('m1', 1, { body: 'see file' }) }, { status: 201 })
      },
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)

    await userEvent.upload(screen.getByLabelText('Upload files'), new File(['hello'], 'note.txt', { type: 'text/plain' }))
    expect(await screen.findByText('note.txt')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('消息'), 'see file{Enter}')

    await waitFor(() => expect(uploaded).toBe('note.txt hello'))
    expect(screen.queryByRole('alert')).toBeNull()
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', body: 'see file', mentions: [], attachment_ids: ['at1'] }))
    expect(uploaded).toBe('note.txt hello')
    await waitFor(() => expect(screen.queryByText('note.txt')).not.toBeInTheDocument())
  })

  it('takes no files pasted or dropped while a message goes out, and takes them once it is sent', async () => {
    let release = () => {}
    let posts = 0
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/messages': async () => {
        posts++
        await new Promise<void>((resolve) => {
          release = resolve
        })
        return Response.json({ message: message('m1', 1, { body: 'first' }) }, { status: 201 })
      },
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    const box = screen.getByLabelText('消息')
    await userEvent.type(box, 'first{Enter}')
    await waitFor(() => expect(posts).toBe(1))

    // Words paste as ever: the paste goes on to the box.
    const words = { items: [{ kind: 'string', type: 'text/plain' }], files: [], types: ['text/plain'], getData: () => 'hi' }
    expect(fireEvent.paste(box, { clipboardData: words })).toBe(true)

    // Files are not taken, and nothing is said about it.
    const shot = new File(['png'], 'shot.png', { type: 'image/png' })
    const files = { items: [{ kind: 'file', type: 'image/png', getAsFile: () => shot }], files: [shot], types: ['Files'], getData: () => '' }
    expect(fireEvent.paste(box, { clipboardData: files })).toBe(false)
    expect(fireEvent.drop(box, { dataTransfer: files })).toBe(false)
    expect(screen.queryByText('shot.png')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()

    // Sent: the files come in.
    release()
    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeEnabled())
    fireEvent.paste(box, { clipboardData: files })
    expect(await screen.findByText('shot.png')).toBeInTheDocument()
  })

  it('keeps the files when the upload fails', async () => {
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/attachments': Response.json({ error: 'disk full' }, { status: 500 }),
    })
    renderWithProviders(<Composer roomId="r1" roomName="main" />)
    await userEvent.upload(screen.getByLabelText('Upload files'), new File(['hello'], 'note.txt', { type: 'text/plain' }))
    await screen.findByText('note.txt')
    await userEvent.click(screen.getByRole('button', { name: '发送' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('服务器出错了，详情请查看服务器日志。')
    expect(screen.getByText('note.txt')).toBeInTheDocument()
  })
})
