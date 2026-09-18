import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { activity, claude, codex, laptop, pi } from '@/test/machines'
import { MachinesPage } from './MachinesPage'

const twoMachines = () => ({
  '/machines': {
    machines: [
      laptop({ runtimes: [{ ...claude, status: 'error', detail: 'version check failed' }, codex, pi] }),
      laptop({ id: 'w2', name: 'build-box', runtimes: [codex, pi] }),
    ],
  },
  '/agents': { agents: [] },
  '/machines/w1/members': { members: [] },
  '/machines/w1/activity': { activity: activity() },
  '/machines/w2/members': { members: [] },
  '/machines/w2/activity': { activity: activity() },
})

function open(route = '/machines') {
  return renderWithProviders(<MachinesPage />, { route, path: route === '/machines' ? '/machines' : '/machines/:machineId' })
}

function rowsOf(list: HTMLElement) {
  return within(list)
    .getAllByRole('listitem')
    .map((row) => within(row).getByRole('link'))
}

describe('MachinesPage', () => {
  it('lists the machines in a column and opens none until one is picked', async () => {
    stubApi(twoMachines())
    const { router } = open()

    expect(await screen.findByText('2 台机器')).toBeInTheDocument()
    const rows = rowsOf((await screen.findAllByRole('list'))[0])
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveAttribute('href', '/machines/w1')
    expect(rows[0]).toHaveTextContent('laptop')
    expect(rows[0]).toHaveTextContent('在线 · 最后心跳 15 秒前')
    // Claude Code's check failed; Codex is not installed and does not count.
    expect(within(rows[0]).getByLabelText('1 个需要处理')).toHaveTextContent('1')
    expect(rows[1]).toHaveAttribute('href', '/machines/w2')
    expect(rows[1]).not.toHaveAttribute('aria-current')
    expect(within(rows[1]).queryByLabelText(/需要处理/)).toBeNull()

    // With nothing in the address, no machine is open until one is picked.
    expect(rows[0]).not.toHaveAttribute('aria-current')
    expect(screen.getByText('选一台机器查看。')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'laptop' })).toBeNull()
    // Checking again belongs to an open machine; the page's bar has none.
    expect(screen.queryByRole('button', { name: '重新检测' })).toBeNull()

    // Picking one opens it.
    await userEvent.click(rows[1])
    expect(router.state.location.pathname).toBe('/machines/w2')
  })

  it('opens the machine the address names', async () => {
    stubApi(twoMachines())
    open('/machines/w2')
    expect(await screen.findByRole('heading', { name: 'build-box' })).toBeInTheDocument()
    const rows = rowsOf((await screen.findAllByRole('list'))[0])
    expect(rows[1]).toHaveAttribute('aria-current', 'page')
    expect(rows[0]).not.toHaveAttribute('aria-current')
  })

  it('says when nobody is connected', async () => {
    stubApi({ '/machines': { machines: [] } })
    open()
    expect(await screen.findByText('没有连上的机器。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重新检测' })).toBeNull()
  })

  it('says so when the machine in the address is not connected', async () => {
    stubApi(twoMachines())
    open('/machines/w9')
    expect(await screen.findByText('这台机器不在线。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '回到机器列表' })).toHaveAttribute('href', '/machines')
    // The list is still there to pick another.
    expect(rowsOf(screen.getAllByRole('list')[0])).toHaveLength(2)
  })
})
