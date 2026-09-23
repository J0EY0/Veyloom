import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { EditProjectDialog } from './EditProjectDialog'

const veyloom = project('p1', 'Veyloom', '/src/veyloom')

describe('EditProjectDialog', () => {
  it('renames the project and moves its checkout', async () => {
    let patched: unknown
    stubApi({
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { ...veyloom, name: 'Platform', repo_path: '/work/platform' }, rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const onClose = vi.fn()
    renderWithProviders(<EditProjectDialog project={veyloom} onClose={onClose} />)

    expect(screen.getByRole('dialog', { name: '编辑项目' })).toBeInTheDocument()
    const name = screen.getByLabelText('名称')
    const path = screen.getByLabelText('本地路径')
    expect(name).toHaveValue('Veyloom')
    expect(path).toHaveValue('/src/veyloom')
    await userEvent.clear(name)
    await userEvent.type(name, ' Platform ')
    await userEvent.clear(path)
    await userEvent.type(path, '/work/platform')
    // What the project is: every agent's brief opens with it.
    const about = screen.getByLabelText('项目简介')
    expect(about).toHaveValue('')
    await userEvent.type(about, ' A chat for coding agents. ')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    expect(patched).toEqual({ name: 'Platform', repo_path: '/work/platform', description: 'A chat for coding agents.' })
  })

  it('chooses who keeps the wiki and when, and says nothing of it when nothing changed', async () => {
    const bodies: unknown[] = []
    stubApi({
      '/rooms/p1-main/members': {
        members: [
          { id: 'm1', display_name: 'Keeper', enabled: true },
          { id: 'm2', display_name: 'Off', enabled: false },
        ],
      },
      '/projects/p1/wiki/maintainer': { upkeep: { trigger: 'idle', idle_minutes: 30, waiting: { own: 0, uses: 0, settled: 0 }, queued: false } },
      '/projects/p1': async (req: Request) => {
        bodies.push(await req.json())
        return { project: veyloom, rooms: [room('p1-main', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    const onClose = vi.fn()
    renderWithProviders(<EditProjectDialog project={veyloom} onClose={onClose} />)
    await user.click(screen.getByRole('combobox', { name: 'Wiki 维护员' }))
    // Members switched off are not offered.
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual(['不开', 'Keeper'])
    await user.click(screen.getByRole('option', { name: 'Keeper' }))
    // Daily is the default, a week the longest (docs/design.md 5.16).
    expect(screen.getByRole('combobox', { name: '什么时候整理' })).toHaveTextContent('每天一次')
    await user.click(screen.getByRole('combobox', { name: '什么时候整理' }))
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual([
      '话题静置 30 分钟后',
      '每天一次',
      '每 3 天一次',
      '每周一次',
      '只在手动时',
    ])
    await user.click(screen.getByRole('option', { name: '每周一次' }))
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    expect(bodies[0]).toMatchObject({ wiki_maintainer_member_id: 'm1', wiki_maintainer_trigger: 'weekly' })
  })

  it('turns the maintainer off', async () => {
    let patched: unknown
    stubApi({
      '/rooms/p1-main/members': { members: [{ id: 'm1', display_name: 'Keeper', enabled: true }] },
      '/projects/p1/wiki/maintainer': { upkeep: { trigger: 'daily', idle_minutes: 30, waiting: { own: 0, uses: 0, settled: 0 }, queued: false } },
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: veyloom, rooms: [room('p1-main', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    const onClose = vi.fn()
    renderWithProviders(<EditProjectDialog project={{ ...veyloom, wiki_maintainer_member_id: 'm1', wiki_maintainer_trigger: 'daily' }} onClose={onClose} />)
    expect(await screen.findByRole('combobox', { name: '什么时候整理' })).toHaveTextContent('每天一次')
    await user.click(screen.getByRole('combobox', { name: 'Wiki 维护员' }))
    await user.click(await screen.findByRole('option', { name: '不开' }))
    expect(screen.queryByRole('combobox', { name: '什么时候整理' })).toBeNull()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    expect(patched).toMatchObject({ wiki_maintainer_member_id: '' })
    expect(patched).not.toHaveProperty('wiki_maintainer_trigger')
  })

  it('mounts the folders given, one a line, and says nothing of them when they stay', async () => {
    const bodies: unknown[] = []
    stubApi({
      '/rooms/p1-main/members': { members: [] },
      '/projects/p1/wiki/maintainer': { upkeep: { trigger: 'idle', idle_minutes: 30, waiting: { own: 0, uses: 0, settled: 0 }, queued: false } },
      '/projects/p1': async (req: Request) => {
        bodies.push(await req.json())
        return { project: veyloom, rooms: [room('p1-main', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    const onClose = vi.fn()
    const mounted = { ...veyloom, wiki_external_bundles: ['/data/catalog'] }
    const { unmount } = renderWithProviders(<EditProjectDialog project={mounted} onClose={onClose} />)
    const mounts = screen.getByLabelText('外部 wiki（只读）')
    expect(mounts).toHaveValue('/data/catalog')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    expect(bodies[0]).not.toHaveProperty('wiki_external_bundles')
    unmount()

    renderWithProviders(<EditProjectDialog project={mounted} onClose={onClose} />)
    const again = screen.getByLabelText('外部 wiki（只读）')
    await user.type(again, '{enter}  /data/runbooks  {enter}{enter}')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toMatchObject({ wiki_external_bundles: ['/data/catalog', '/data/runbooks'] })
  })

  it('renames alone when asked to', async () => {
    let patched: unknown
    stubApi({
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { ...veyloom, name: 'Platform' }, rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const onClose = vi.fn()
    renderWithProviders(<EditProjectDialog project={veyloom} rename onClose={onClose} />)
    expect(screen.getByRole('dialog', { name: '重命名项目' })).toBeInTheDocument()
    expect(screen.queryByLabelText('本地路径')).toBeNull()
    await userEvent.clear(screen.getByLabelText('名称'))
    await userEvent.type(screen.getByLabelText('名称'), 'Platform')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    // The checkout stays as it is: the request does not name it.
    expect(patched).toEqual({ name: 'Platform' })
  })

  it('refuses an empty name without calling the server, and shows what the server says', async () => {
    const calls = stubApi({ '/projects/p1': Response.json({ error: 'project p1: not found' }, { status: 404 }) })
    renderWithProviders(<EditProjectDialog project={veyloom} onClose={() => {}} />)

    const name = screen.getByLabelText('名称')
    await userEvent.clear(name)
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('项目名不能为空。')
    expect(name).toHaveFocus()
    expect(calls.filter((call) => call.startsWith('PATCH'))).toEqual([])

    await userEvent.type(name, 'Veyloom')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('找不到了，可能已经被删除。')
  })
})
