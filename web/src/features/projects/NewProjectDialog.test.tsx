import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { NewProjectDialog } from './NewProjectDialog'

const agent = (id: string, name: string, runtime: string, machine_id = 'w1', machine_name = 'laptop') => ({
  id,
  name,
  avatar: '',
  machine_id,
  machine_name,
  projects: [],
  runtime,
  model: '',
  role_card: '',
  permission_preset: 'read_only',
  runtime_options: null,
})

const agents = [agent('ag1', 'Claude Architect', 'claude'), agent('ag2', 'Pi Tester', 'pi'), agent('ag3', 'Codex Helper', 'codex', 'w2', 'build-box')]

function stub(extra: Record<string, unknown> = {}) {
  return stubApi({
    '/agents': { agents },
    // build-box is not connected.
    '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [] }] },
    ...extra,
  })
}

describe('NewProjectDialog', () => {
  it('starts the project with the agents picked, in their checkout, and opens its chat', async () => {
    let posted: unknown
    stub({
      '/projects': async (req: Request) => {
        posted = await req.json()
        return Response.json({ project: project('p9', 'New', '/src/new'), rooms: [room('r9', 'p9', 'main')] }, { status: 201 })
      },
    })
    const onClose = vi.fn()
    const { router } = renderWithProviders(<NewProjectDialog open onClose={onClose} />)

    await userEvent.type(screen.getByLabelText('名称'), 'New')
    await userEvent.type(screen.getByLabelText('本地路径'), ' /src/new ')
    // Each agent says where it would run, by machine and runtime.
    const tester = await screen.findByRole('checkbox', { name: /Pi Tester/ })
    expect(screen.getByRole('checkbox', { name: /Codex Helper/ })).toHaveAccessibleName(/build-box（不在线） · Codex/)
    expect(tester).toHaveAccessibleName(/laptop · Pi/)
    // Picked out of order, they are sent in the order of the list.
    await userEvent.click(tester)
    await userEvent.click(screen.getByRole('checkbox', { name: /Claude Architect/ }))
    expect(screen.getByRole('group', { name: /Agent/ })).toHaveTextContent('已选 2')
    await userEvent.click(screen.getByRole('button', { name: '创建项目' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/rooms/r9'))
    expect(posted).toEqual({ name: 'New', repo_path: '/src/new', agent_ids: ['ag1', 'ag2'] })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('may start with one of the agents picked keeping the wiki, none by default', async () => {
    const posted: unknown[] = []
    stub({
      '/projects': async (req: Request) => {
        posted.push(await req.json())
        return Response.json({ project: project('p9', 'New'), rooms: [room('r9', 'p9', 'main')] }, { status: 201 })
      },
    })
    renderWithProviders(<NewProjectDialog open onClose={() => {}} />)
    await userEvent.type(screen.getByLabelText('名称'), 'New')
    // Nothing to choose from before an agent is picked.
    const tester = await screen.findByRole('checkbox', { name: /Pi Tester/ })
    expect(screen.queryByRole('combobox', { name: 'Wiki 维护员' })).toBeNull()
    await userEvent.click(tester)
    await userEvent.click(screen.getByRole('checkbox', { name: /Claude Architect/ }))
    const keeper = screen.getByRole('combobox', { name: 'Wiki 维护员' })
    expect(keeper).toHaveTextContent('暂不选')
    await userEvent.click(keeper)
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual(['暂不选', 'Claude Architect', 'Pi Tester'])
    await userEvent.click(screen.getByRole('option', { name: 'Pi Tester' }))
    await userEvent.click(screen.getByRole('button', { name: '创建项目' }))
    await waitFor(() => expect(posted[0]).toMatchObject({ agent_ids: ['ag1', 'ag2'], wiki_maintainer_agent_id: 'ag2' }))
  })

  it('picks the way an IM starts a group chat: chips in the search box, Enter and Backspace', async () => {
    const calls = stub()
    renderWithProviders(<NewProjectDialog open onClose={() => {}} />)
    const search = await screen.findByRole('searchbox', { name: '搜索 Agent' })

    // The search reads the name, the machine and the runtime.
    await userEvent.type(search, 'build')
    expect(screen.getAllByRole('checkbox')).toHaveLength(1)
    // Enter picks the first match and empties the box; it creates nothing.
    await userEvent.keyboard('{Enter}')
    expect(search).toHaveValue('')
    expect(screen.getAllByRole('checkbox')).toHaveLength(3)
    expect(screen.getByRole('checkbox', { name: /Codex Helper/ })).toBeChecked()

    // The picked sit in the box as chips, in the order they were picked.
    await userEvent.click(screen.getByRole('checkbox', { name: /Pi Tester/ }))
    const box = search.parentElement as HTMLElement
    expect(
      within(box)
        .getAllByRole('button')
        .map((chip) => chip.getAttribute('aria-label')),
    ).toEqual(['移除 Codex Helper', '移除 Pi Tester'])

    // Backspace in the empty box drops the last chip, a click on a chip that one.
    await userEvent.click(search)
    await userEvent.keyboard('{Backspace}')
    expect(screen.getByRole('checkbox', { name: /Pi Tester/ })).not.toBeChecked()
    await userEvent.click(within(box).getByRole('button', { name: '移除 Codex Helper' }))
    expect(screen.getByRole('checkbox', { name: /Codex Helper/ })).not.toBeChecked()
    expect(within(box).queryAllByRole('button')).toEqual([])

    // An Enter that confirms a word in an input method is left alone.
    await userEvent.type(search, 'codex')
    fireEvent.keyDown(search, { key: 'Enter', isComposing: true })
    expect(search).toHaveValue('codex')
    expect(screen.getByRole('checkbox', { name: /Codex Helper/ })).not.toBeChecked()

    await userEvent.type(search, ' nobody')
    expect(screen.getByText('没有匹配的 Agent')).toBeInTheDocument()
    expect(calls.filter((call) => call.startsWith('POST'))).toEqual([])
  })

  it('will not create a project without an agent', async () => {
    const calls = stub()
    renderWithProviders(<NewProjectDialog open onClose={() => {}} />)
    await userEvent.type(screen.getByLabelText('名称'), 'New')

    const create = screen.getByRole('button', { name: '创建项目' })
    expect(create).toBeDisabled()
    const architect = await screen.findByRole('checkbox', { name: /Claude Architect/ })
    await userEvent.click(architect)
    expect(create).toBeEnabled()
    await userEvent.click(architect)
    expect(create).toBeDisabled()
    expect(calls.filter((call) => call.startsWith('POST'))).toEqual([])
  })

  it('points to the Agents page while there is no agent to pick', async () => {
    stubApi({ '/agents': { agents: [] }, '/machines': { machines: [] } })
    const onClose = vi.fn()
    const { router } = renderWithProviders(<NewProjectDialog open onClose={onClose} />)

    const empty = (await screen.findByText('还没有 Agent，先建一个再来。')).closest('[data-slot="empty"]') as HTMLElement
    expect(screen.getByRole('button', { name: '创建项目' })).toBeDisabled()
    await userEvent.click(within(empty).getByRole('link', { name: '去新建 Agent' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/agents'))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('refuses an empty name without calling the server', async () => {
    const calls = stub()
    renderWithProviders(<NewProjectDialog open onClose={() => {}} />)

    await userEvent.click(await screen.findByRole('checkbox', { name: /Pi Tester/ }))
    await userEvent.click(screen.getByRole('button', { name: '创建项目' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('项目名不能为空。')
    expect(screen.getByLabelText('名称')).toHaveFocus()
    expect(calls.filter((call) => call.startsWith('POST'))).toEqual([])
  })

  it('shows the server error inline', async () => {
    stub({ '/projects': Response.json({ error: 'add agent ag2 to "x": agent ag2: not found' }, { status: 404 }) })
    renderWithProviders(<NewProjectDialog open onClose={() => {}} />)

    await userEvent.type(screen.getByLabelText('名称'), 'x')
    await userEvent.click(await screen.findByRole('checkbox', { name: /Pi Tester/ }))
    await userEvent.click(screen.getByRole('button', { name: '创建项目' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('找不到了，可能已经被删除。')
  })
})
