import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { AppSidebar } from './AppSidebar'

function stubSidebar(extra: Record<string, unknown> = {}) {
  return stubApi({
    '/users': { users: [] },
    '/projects': { projects: [project('p1', 'Veyloom', '', 'r1'), project('p2', 'docs-site', '', 'r2')] },
    '/rooms/r2': { room: room('r2', 'p2', 'main') },
    '/approvals': {
      approvals: [
        {
          id: 'ap1',
          turn_id: 'x1',
          room_id: 'r2',
          thread_id: 't1',
          member_id: 'a1',
          request_id: 'q',
          kind: 'tool_use',
          tool: 'Bash',
          input: {},
          status: 'pending',
          created_at: '2026-09-15T08:00:00Z',
          member_name: 'Careful',
          project_name: 'docs-site',
        },
      ],
    },
    '/topics': {
      topics: [
        {
          thread_id: 't9',
          room_id: 'r1',
          root_message_id: 'm9',
          root_body: '把 WebSocket 掉队的处理补上测试',
          members: ['Codex Implementer'],
          started_at: '2026-09-15T08:00:00Z',
        },
      ],
    },
    ...extra,
  })
}

describe('AppSidebar', () => {
  it('lists every project as a chat and marks the one on screen', async () => {
    stubSidebar()
    renderWithProviders(<AppSidebar />, { route: '/rooms/r2' })

    const current = await screen.findByRole('link', { name: /docs-site/ })
    expect(current).toHaveAttribute('href', '/rooms/r2')
    expect(current).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: /Veyloom/ })).toHaveAttribute('href', '/rooms/r1')
    expect(screen.getByRole('link', { name: /Veyloom/ })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('link', { name: '收件箱' })).toHaveAttribute('href', '/inbox')
    // One pending approval anywhere counts in the inbox.
    await waitFor(() => expect(screen.getByRole('link', { name: '收件箱' }).closest('li')).toHaveTextContent('1'))
    // Every project is listed right here, so there is no page of them.
    expect(screen.queryByRole('link', { name: '全部项目' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Agents' })).toHaveAttribute('href', '/agents')
    expect(screen.getByRole('link', { name: '机器' })).toHaveAttribute('href', '/machines')
    expect(screen.getByRole('link', { name: 'Wiki' })).toHaveAttribute('href', '/wiki')
    expect(screen.getByRole('link', { name: '用量' })).toHaveAttribute('href', '/usage')
  })

  it('marks the Wiki page on any of its addresses, not a chat’s Wiki tab', async () => {
    stubSidebar()
    const { unmount } = renderWithProviders(<AppSidebar />, { route: '/wiki/p1/facts/port.md' })
    expect(await screen.findByRole('link', { name: 'Wiki' })).toHaveAttribute('aria-current', 'page')
    unmount()
    renderWithProviders(<AppSidebar />, { route: '/rooms/r1/wiki' })
    expect(await screen.findByRole('link', { name: 'Wiki' })).not.toHaveAttribute('aria-current')
  })

  it('hangs the topics agents are working on under their project', async () => {
    stubSidebar()
    renderWithProviders(<AppSidebar />, { route: '/rooms/r1?thread=t9' })
    const topic = await screen.findByRole('link', { name: /把 WebSocket 掉队的处理补上测试/ })
    expect(topic).toHaveAttribute('href', '/rooms/r1?thread=t9')
    expect(topic).toHaveAttribute('aria-current', 'page')
    // What it says is all there is: no hover title repeating it.
    expect(topic).not.toHaveAttribute('title')
    // Under Veyloom (its chat is r1), not under docs-site.
    const veyloom = screen.getByRole('link', { name: /Veyloom/ }).closest('li') as HTMLElement
    expect(veyloom).toContainElement(topic)
  })

  it('renames and deletes a project from the menu on its row', async () => {
    let patched: unknown
    const calls = stubSidebar({
      '/projects/p2': async (req: Request) => {
        if (req.method === 'DELETE') return new Response(null, { status: 204 })
        patched = await req.json()
        return { project: project('p2', 'docs', '', 'r2'), rooms: [room('r2', 'p2', 'main')] }
      },
    })
    const { router } = renderWithProviders(<AppSidebar />, { route: '/rooms/r2' })

    await userEvent.click(await screen.findByRole('button', { name: 'docs-site 的菜单' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '重命名' }))
    // A rename asks for the name alone.
    const rename = await screen.findByRole('dialog', { name: '重命名项目' })
    expect(within(rename).queryByLabelText('本地路径')).toBeNull()
    const name = within(rename).getByLabelText('名称')
    await userEvent.clear(name)
    await userEvent.type(name, 'docs')
    await userEvent.click(within(rename).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(patched).toEqual({ name: 'docs' }))
    expect(await screen.findByRole('link', { name: 'docs' })).toBeInTheDocument()

    // Deleting asks first, names what goes with it, and leaves the chat that was on screen.
    await userEvent.click(screen.getByRole('button', { name: 'docs 的菜单' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除' }))
    const confirm = await screen.findByRole('alertdialog', { name: '删除「docs」和它的全部群聊记录？' })
    expect(calls.filter((call) => call.startsWith('DELETE'))).toEqual([])
    await userEvent.click(within(confirm).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(calls).toContain('DELETE /projects/p2')
    expect(screen.queryByRole('link', { name: 'docs' })).toBeNull()
    expect(router.state.location.pathname).toBe('/')
  })

  it('keeps a project while a member works in it', async () => {
    stubSidebar({ '/projects/p1': () => Response.json({ error: 'a turn is still running' }, { status: 409 }) })
    renderWithProviders(<AppSidebar />)
    await userEvent.click(await screen.findByRole('button', { name: 'Veyloom 的菜单' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除' }))
    const confirm = await screen.findByRole('alertdialog')
    await userEvent.click(within(confirm).getByRole('button', { name: '删除' }))
    expect(await within(confirm).findByRole('alert')).toHaveTextContent('项目里还有成员在工作，请等这一轮结束后再删除。')
    await userEvent.click(within(confirm).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(screen.getByRole('link', { name: 'Veyloom' })).toBeInTheDocument()
  })

  it('offers to create the first project when there is none', async () => {
    stubApi({ '/users': { users: [] }, '/projects': { projects: [] }, '/approvals': { approvals: [] }, '/topics': { topics: [] } })
    renderWithProviders(<AppSidebar />)
    const group = await screen.findByRole('list', { name: '项目' })
    await userEvent.click(within(group).getByRole('button', { name: '新建项目' }))
    expect(await screen.findByRole('dialog', { name: '新建项目' })).toBeInTheDocument()
  })

  it('opens the new project dialog from the header', async () => {
    stubSidebar()
    renderWithProviders(<AppSidebar />)
    await screen.findByRole('link', { name: /Veyloom/ })
    await userEvent.click(screen.getAllByRole('button', { name: '新建项目' })[0])
    expect(await screen.findByRole('dialog', { name: '新建项目' })).toBeInTheDocument()
  })
})

describe('AppSidebar in English', () => {
  it('reads in English once the language is switched', async () => {
    stubSidebar()
    const { setLocale } = await import('@/lib/i18n')
    setLocale('en')
    renderWithProviders(<AppSidebar />)
    expect(await screen.findByRole('link', { name: 'Inbox' })).toHaveAttribute('href', '/inbox')
    expect(screen.getByRole('link', { name: 'Agents' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Signing in…' })).toBeInTheDocument()
  })
})
