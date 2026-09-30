import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { pickOption } from '@/test/select'
import { MembersPanel } from './MembersPanel'

const codex = {
  id: 'a1',
  room_id: 'r1',
  agent_id: 't1',
  machine_id: 'w1',
  display_name: 'Codex Implementer',
  repo_path: '/src/veyloom',
  model: '',
  permission_preset: '',
  enabled: true,
}

function stubPanel(extra: Record<string, unknown> = {}) {
  return stubApi({
    '/rooms/r1/members': { members: [codex] },
    '/rooms/r1/approvals': { approvals: [] },
    '/rooms/r1/turns': { turns: [] },
    '/agents': {
      agents: [
        {
          id: 't1',
          name: 'Implementer',
          machine_id: 'w1',
          machine_name: 'laptop',
          projects: [],
          runtime: 'codex',
          model: 'gpt-5',
          permission_preset: 'edit_with_approval',
        },
      ],
    },
    '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [{ name: 'codex' }] }] },
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/projects': { projects: [project('p1', 'Veyloom', '/src/veyloom')] },
    ...extra,
  })
}

function renderPanel() {
  return renderWithProviders(<MembersPanel roomId="r1" roomName="main" onClose={vi.fn()} onOpenThread={vi.fn()} />)
}

describe('MembersPanel', () => {
  it('lists each member with what it is doing now', async () => {
    stubPanel()
    renderPanel()
    expect(await screen.findByText('Codex Implementer')).toBeInTheDocument()
    expect(await screen.findByText('空闲')).toBeInTheDocument()
  })

  it('opens with the project the chat belongs to, then its members', async () => {
    stubPanel()
    renderPanel()
    const panel = screen.getByRole('complementary', { name: '群聊信息' })
    expect(await within(panel).findByText('Veyloom')).toBeInTheDocument()
    expect(within(panel).getByText('/src/veyloom')).toBeInTheDocument()
    const section = within(panel).getByRole('region', { name: '成员' })
    expect(await within(section).findByText('Codex Implementer')).toBeInTheDocument()
    expect(within(section).getByRole('heading', { name: '成员' }).parentElement).toHaveTextContent('成员1')
    expect(within(section).getByRole('button', { name: '添加成员' })).toBeInTheDocument()
    // A handful of members needs no search.
    expect(within(section).queryByRole('searchbox')).toBeNull()

    // The project card opens the project for editing.
    await userEvent.click(within(panel).getByRole('button', { name: '编辑项目 Veyloom' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑项目' })
    expect(within(dialog).getByLabelText('本地路径')).toHaveValue('/src/veyloom')
  })

  it('searches the members once there are many', async () => {
    const names = ['Architect', 'Builder', 'Checker', 'Deployer', 'Editor', 'Fixer']
    stubPanel({ '/rooms/r1/members': { members: names.map((name, i) => ({ ...codex, id: `m${i}`, display_name: name })) } })
    renderPanel()
    await screen.findByText('Fixer')
    const search = screen.getByRole('searchbox', { name: '搜索成员' })
    await userEvent.type(search, 'er')
    expect(screen.getAllByRole('listitem').map((row) => row.textContent)).toEqual([
      expect.stringContaining('Builder'),
      expect.stringContaining('Checker'),
      expect.stringContaining('Deployer'),
      expect.stringContaining('Fixer'),
    ])
    await userEvent.type(search, 'zzz')
    expect(screen.getByText('没有匹配的成员')).toBeInTheDocument()
  })

  it('puts whoever needs a person first, then whoever is busy', async () => {
    stubPanel({
      '/rooms/r1/members': {
        members: [
          { ...codex, id: 'a1', display_name: 'Idle one' },
          { ...codex, id: 'a2', display_name: 'Busy one' },
          { ...codex, id: 'a3', display_name: 'Waiting one' },
        ],
      },
      '/rooms/r1/turns': { turns: [{ id: 'turn1', member_id: 'a2', thread_id: 'th1', status: 'running', started_at: new Date().toISOString() }] },
      '/rooms/r1/approvals': { approvals: [{ id: 'ap1', member_id: 'a3', thread_id: 'th2', status: 'pending', kind: 'command', payload: {} }] },
    })
    renderPanel()
    await screen.findByText('Waiting one')
    const names = screen.getAllByRole('listitem').map((li) => li.textContent)
    expect(names[0]).toContain('Waiting one')
    expect(names[1]).toContain('Busy one')
    expect(names[2]).toContain('Idle one')
  })

  it('says why a member waits under a pause, and resumes it from its menu', async () => {
    let lifted = ''
    stubPanel({
      '/pauses': { pauses: [{ id: 'p1', machine_id: 'w1', runtime: 'codex', reason: 'auth', detail: 'unauthorized', created_at: '' }] },
      '/pauses/p1': (req: Request) => ((lifted = req.method), new Response(null, { status: 204 })),
    })
    renderPanel()
    expect(await screen.findByText('登录失效')).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '继续' }))
    await waitFor(() => expect(lifted).toBe('DELETE'))
  })

  it('switches a member off from its menu', async () => {
    let patched: unknown
    stubPanel({
      '/members/a1': async (req: Request) => {
        patched = await req.json()
        return { member: { ...codex, enabled: false } }
      },
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '停用' }))
    await waitFor(() => expect(patched).toEqual({ enabled: false }))
    expect(await screen.findByText('已停用')).toBeInTheDocument()
  })

  it('marks the leader, and makes another member the leader from its menu', async () => {
    let patched: unknown
    const pi = { ...codex, id: 'a2', display_name: 'Pi Tester' }
    const veyloom = project('p1', 'Veyloom', '/src/veyloom')
    stubPanel({
      '/rooms/r1/members': { members: [codex, pi] },
      '/projects': { projects: [{ ...veyloom, leader_id: 'a1' }] },
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { ...veyloom, leader_id: 'a2', leader_member_id: 'a2' }, rooms: [room('r1', 'p1', 'main')] }
      },
    })
    renderPanel()
    const first = (await screen.findByText('Codex Implementer')).closest('[role=listitem]') as HTMLElement
    const other = screen.getByText('Pi Tester').closest('[role=listitem]') as HTMLElement
    await waitFor(() => expect(first).toHaveTextContent('组长'))
    expect(other).not.toHaveTextContent('组长')
    // The leader is not made the leader again.
    await userEvent.click(within(first).getByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '编辑' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '设为组长' })).toBeNull()
    await userEvent.keyboard('{Escape}')
    await userEvent.click(within(other).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '设为组长' }))
    await waitFor(() => expect(patched).toEqual({ leader_member_id: 'a2' }))
    await waitFor(() => expect(other).toHaveTextContent('组长'))
    expect(first).not.toHaveTextContent('组长')
  })

  it('adds a member from an agent, which runs on its own machine, in the project checkout', async () => {
    let posted: unknown
    stubPanel({
      '/rooms/r1/members': async (req: Request) => {
        if (req.method === 'POST') {
          posted = await req.json()
          return Response.json({ member: { ...codex, id: 'a2', display_name: 'Second' } }, { status: 201 })
        }
        return { members: [codex] }
      },
      '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [{ name: 'codex' }, { name: 'fake' }] }] },
      '/rooms/r1': { room: room('r1', 'p1', 'main') },
      '/projects': { projects: [project('p1', 'Veyloom', '/src/veyloom')] },
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '添加成员' }))
    // Named as the button that opened it: what joins a chat is a member.
    expect(await screen.findByRole('dialog', { name: '添加成员' })).toBeInTheDocument()
    // The repository starts as where the project is checked out.
    expect(await screen.findByDisplayValue('/src/veyloom')).toHaveAccessibleName('仓库路径')

    await screen.findByLabelText('Agent')
    // An agent reads as its name, the machine it is on and its runtime; there
    // is no machine to pick for the member.
    await userEvent.click(screen.getByLabelText('Agent'))
    const agent = await screen.findByRole('option', { name: /Implementer/ })
    expect(agent).toHaveTextContent(/^Implementer · laptop · Codex · gpt-5 · /)
    await userEvent.click(agent)
    expect(screen.queryByLabelText('机器')).toBeNull()
    await userEvent.type(screen.getByLabelText('显示名'), 'Second')
    await pickOption('权限', '完全信任')
    await userEvent.click(screen.getByRole('button', { name: '添加到 main' }))

    await waitFor(() =>
      expect(posted).toEqual({ agent_id: 't1', display_name: 'Second', repo_path: '/src/veyloom', model: '', permission_preset: 'full_auto' }),
    )
    expect(await screen.findByText('Second')).toBeInTheDocument()
  })

  it('takes a member out of the project once confirmed', async () => {
    const calls = stubPanel({
      '/members/a1': (req: Request) => (req.method === 'DELETE' ? new Response(null, { status: 204 }) : { member: codex }),
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '移出项目' }))

    const confirm = await screen.findByRole('alertdialog', { name: '将「Codex Implementer」移出 main？' })
    expect(calls.filter((call) => call.startsWith('DELETE'))).toEqual([])
    await userEvent.click(within(confirm).getByRole('button', { name: '移出' }))

    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(calls).toContain('DELETE /members/a1')
    expect(await screen.findByText('这个项目还没有成员。')).toBeInTheDocument()
  })

  it('will not take out a member while it works', async () => {
    stubPanel({
      '/rooms/r1/turns': { turns: [{ id: 'turn1', member_id: 'a1', thread_id: 'th1', status: 'running', started_at: new Date().toISOString() }] },
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '工作中，无法移出' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('says so when the hub finds it busy after all', async () => {
    stubPanel({
      '/members/a1': () => Response.json({ error: 'a turn is still running' }, { status: 409 }),
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '移出项目' }))
    const confirm = await screen.findByRole('alertdialog')
    await userEvent.click(within(confirm).getByRole('button', { name: '移出' }))
    expect(await within(confirm).findByRole('alert')).toHaveTextContent('它正在工作，请等这一轮结束后再移出。')
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
  })

  it('starts a new session for a member from its menu', async () => {
    const calls = stubPanel({ '/members/a1/session': new Response(null, { status: 204 }) })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '开启新会话' }))
    await waitFor(() => expect(calls).toContain('DELETE /members/a1/session'))
  })

  it('will not start a new session from under a running turn', async () => {
    stubPanel({
      '/rooms/r1/turns': { turns: [{ id: 'turn1', member_id: 'a1', thread_id: 'th1', status: 'running', started_at: new Date().toISOString() }] },
    })
    renderPanel()
    await userEvent.click(await screen.findByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '工作中，无法开启新会话' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('tells how long a member has been in its session', async () => {
    stubPanel({
      '/members/a1/session': {
        session: {
          id: 's1',
          member_id: 'a1',
          runtime: 'codex',
          work_dir: '/src/veyloom',
          compactions: 2,
          started_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
        },
        turns: 53,
      },
    })
    renderPanel()
    await userEvent.click(await screen.findByText('Codex Implementer'))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText(/已运行 53 轮，上下文压缩过 2 次/)).toBeInTheDocument()
    // A session belongs to the directory it was opened in.
    expect(within(dialog).getByText('修改路径后，下一轮会开启新会话。')).toBeInTheDocument()
  })

  it('says a member has no session before its first turn', async () => {
    stubPanel({ '/members/a1/session': { turns: 0 } })
    renderPanel()
    await userEvent.click(await screen.findByText('Codex Implementer'))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('还没有会话，下一轮开始时会新建。')).toBeInTheDocument()
    expect(within(dialog).queryByText('修改路径后，下一轮会开启新会话。')).not.toBeInTheDocument()
  })
})
