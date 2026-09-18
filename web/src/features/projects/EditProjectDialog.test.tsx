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
    const calls = stubApi({ '/projects/p1': Response.json({ error: 'project p1: store: not found' }, { status: 404 }) })
    renderWithProviders(<EditProjectDialog project={veyloom} onClose={() => {}} />)

    const name = screen.getByLabelText('名称')
    await userEvent.clear(name)
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('项目名不能为空。')
    expect(name).toHaveFocus()
    expect(calls.filter((call) => call.startsWith('PATCH'))).toEqual([])

    await userEvent.type(name, 'Veyloom')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('project p1: store: not found')
  })
})
