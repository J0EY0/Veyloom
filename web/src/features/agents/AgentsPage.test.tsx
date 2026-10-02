import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { AgentsPage } from './AgentsPage'

const machine = { avatar: '', machine_id: 'w1', machine_name: 'laptop', projects: [] }
const agents = [
  { id: 't1', name: 'Claude Architect', runtime: 'claude', model: 'opus', permission_preset: 'read_only', runtime_options: null, ...machine },
  { id: 't2', name: 'Fake Implementer', runtime: 'fake', model: '', permission_preset: 'edit_with_approval', runtime_options: {}, ...machine },
]

describe('AgentsPage', () => {
  it('lists roles with runtime, model and permission; a row opens it in a dialog', async () => {
    stubApi({ '/agents': { agents }, '/machines': { machines: [] } })
    renderWithProviders(<AgentsPage />)
    const row = await screen.findByRole('button', { name: /Claude Architect/ })
    // The name wears the runtime's colour; a screen reader hears the runtime
    // by product name. Nothing pops up over what the card already shows.
    expect(row).toHaveAccessibleName(/Claude Code/)
    await userEvent.hover(within(row).getByText('Claude Architect'))
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    // Model and permission read as one line along the bottom.
    expect(row).toHaveTextContent('opus')
    expect(row).toHaveTextContent('只读')
    expect(screen.getByText('需要审批')).toBeInTheDocument()

    await userEvent.click(row)
    expect(await screen.findByRole('dialog', { name: /Claude Architect/ })).toBeInTheDocument()
    expect(screen.getByLabelText('名称')).toHaveValue('Claude Architect')
    expect(screen.getByRole('button', { name: '保存' })).toBeInTheDocument()
  })

  it("leads each card with its avatar: its picture, or else its name's letter; the runtime's mark in the corner", async () => {
    const picture = '0123456789abcdef0123456789abcdef.webp'
    stubApi({ '/agents': { agents: [agents[1], { ...agents[1], id: 't3', name: 'Fake Reviewer', avatar: picture }] }, '/machines': { machines: [] } })
    renderWithProviders(<AgentsPage />)

    // No picture: the name's letter, and the fake runtime's mark, its own
    // letter, in the corner.
    const plain = await screen.findByRole('button', { name: /Fake Implementer/ })
    expect(plain.querySelector('[data-avatar]')).toBeNull()
    expect(within(plain).getAllByText('F')).toHaveLength(2)
    // A picture leads, and the corner still says which runtime it is.
    const pictured = screen.getByRole('button', { name: /Fake Reviewer/ })
    expect(pictured.querySelector(`[data-avatar="${picture}"]`)).not.toBeNull()
    expect(
      within(pictured)
        .getAllByText('F')
        .filter((mark) => !mark.closest('[data-avatar]')),
    ).toHaveLength(1)
  })

  it('narrows the grid by search and by runtime', async () => {
    stubApi({ '/agents': { agents }, '/machines': { machines: [] } })
    renderWithProviders(<AgentsPage />)
    await screen.findByRole('button', { name: /Claude Architect/ })

    // Search reads the name, the model and the role card.
    await userEvent.type(screen.getByLabelText('搜索 Agent'), 'Fake')
    expect(screen.queryByRole('button', { name: /Claude Architect/ })).toBeNull()
    expect(screen.getByRole('button', { name: /Fake Implementer/ })).toBeInTheDocument()

    // Nothing left says so, and offers a way back.
    await userEvent.clear(screen.getByLabelText('搜索 Agent'))
    await userEvent.type(screen.getByLabelText('搜索 Agent'), 'zzz')
    expect(await screen.findByText('没有匹配的 Agent')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '清空筛选' }))

    // The runtime menu lists the runtimes actually in use.
    await userEvent.click(screen.getByRole('button', { name: /运行时：全部/ }))
    await userEvent.click(await screen.findByRole('menuitemradio', { name: 'Claude Code' }))
    expect(await screen.findByRole('button', { name: /Claude Architect/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /运行时：Claude Code/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Fake Implementer/ })).toBeNull()
  })

  it('greets an empty library with one line and a way to make the first', async () => {
    stubApi({ '/agents': { agents: [] }, '/machines': { machines: [] } })
    renderWithProviders(<AgentsPage />)
    const empty = (await screen.findByText('还没有 Agent。')).closest('[data-slot="empty"]') as HTMLElement
    // A title and a button, no sentence under the title.
    expect(empty.querySelector('[data-slot="empty-description"]')).toBeNull()
    expect(within(empty).getByRole('button', { name: '新建 Agent' })).toBeInTheDocument()
    // Nothing to search or filter yet.
    expect(screen.queryByLabelText('搜索 Agent')).toBeNull()
  })

  it('opens an empty dialog from the new button', async () => {
    stubApi({ '/agents': { agents }, '/machines': { machines: [] } })
    renderWithProviders(<AgentsPage />)
    await userEvent.click(await screen.findByRole('button', { name: '新建 Agent' }))
    expect(await screen.findByRole('dialog', { name: '新建 Agent' })).toBeInTheDocument()
    expect(screen.getByLabelText('名称')).toHaveValue('')
    expect(screen.getByRole('button', { name: '创建 Agent' })).toBeInTheDocument()
  })

  it('deletes an agent from its menu once confirmed', async () => {
    const calls = stubApi({
      '/agents': { agents },
      '/agents/t2': (req) => (req.method === 'DELETE' ? new Response(null, { status: 204 }) : { agent: agents[1] }),
    })
    renderWithProviders(<AgentsPage />)
    const card = (await screen.findByRole('button', { name: /Fake Implementer/ })).parentElement!
    await userEvent.click(within(card).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除' }))

    // Nothing is gone until the question is answered.
    const confirm = await screen.findByRole('alertdialog', { name: '删除「Fake Implementer」？' })
    expect(calls.filter((call) => call.startsWith('DELETE'))).toEqual([])
    await userEvent.click(within(confirm).getByRole('button', { name: '删除' }))

    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(calls).toContain('DELETE /agents/t2')
    expect(screen.queryByRole('button', { name: /Fake Implementer/ })).toBeNull()
    expect(screen.getByRole('button', { name: /Claude Architect/ })).toBeInTheDocument()
  })

  it('greys deleting an agent that is still in a project', async () => {
    stubApi({ '/agents': { agents: [{ ...agents[1], projects: ['Veyloom'] }] } })
    renderWithProviders(<AgentsPage />)
    const card = (await screen.findByRole('button', { name: /Fake Implementer/ })).parentElement!
    await userEvent.click(within(card).getByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '删除' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('treats an agent someone else already deleted as deleted', async () => {
    stubApi({
      '/agents': { agents },
      '/agents/t2': () => Response.json({ error: 'agent t2: not found' }, { status: 404 }),
    })
    renderWithProviders(<AgentsPage />)
    const card = (await screen.findByRole('button', { name: /Fake Implementer/ })).parentElement!
    await userEvent.click(within(card).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除' }))
    await userEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: '删除' }))

    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(screen.queryByRole('button', { name: /Fake Implementer/ })).toBeNull()
  })

  it('keeps an agent that is still in a project, and says where', async () => {
    stubApi({
      '/agents': { agents },
      '/agents/t1': () => Response.json({ error: 'still a member of Veyloom, docs-site', projects: ['Veyloom', 'docs-site'] }, { status: 409 }),
    })
    renderWithProviders(<AgentsPage />)
    const card = (await screen.findByRole('button', { name: /Claude Architect/ })).parentElement!
    await userEvent.click(within(card).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除' }))
    const confirm = await screen.findByRole('alertdialog')
    await userEvent.click(within(confirm).getByRole('button', { name: '删除' }))

    expect(await within(confirm).findByRole('alert')).toHaveTextContent('这个 Agent 还在 Veyloom、docs-site 中，请先在成员面板将其移出，再删除。')
    expect(within(confirm).getByRole('button', { name: '删除' })).toBeDisabled()
    await userEvent.click(within(confirm).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(screen.getByRole('button', { name: /Claude Architect/ })).toBeInTheDocument()
  })
})
