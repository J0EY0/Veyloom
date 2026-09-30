import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { LibraryPage } from './LibraryPage'
import { routes, skill } from './library/libraryTesting'

// A skill of this machine the library's copy came from is taken in again
// from the import (docs/design.md 5.15).
describe('updating a skill from where it came from', () => {
  it('takes a skill in again from the folder it came from, asking first when the copy changed since', async () => {
    const updated: string[] = []
    let local = [
      {
        name: 'notes',
        description: 'd',
        folder: '/home/me/.claude/skills/notes',
        where: '~/.claude/skills',
        in_library: true,
        origin: 'changed',
        edits_since: 2,
      },
      { name: 'pdf', description: 'd', folder: '/home/me/.agents/skills/pdf', where: '~/.agents/skills', in_library: true, origin: 'unknown' },
      {
        name: 'go-table-tests',
        description: 'd',
        folder: '/repo/.agents/skills/go-table-tests',
        where: 'Veyloom · .agents/skills',
        in_library: true,
        origin: 'same',
      },
    ]
    stubApi(
      routes({
        '/agents': { agents: [] },
        '/library/local': () => ({ skills: local }),
        '/library/update': async (req: Request) => {
          const { folder } = (await req.json()) as { folder: string }
          updated.push(folder)
          local = local.map((one) => (one.folder === folder ? { ...one, origin: 'same', edits_since: 0 } : one))
          return { page: { ...skill, path: `/skills/${folder.split('/').pop()}/SKILL.md` } }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    await userEvent.click(await screen.findByRole('button', { name: '导入技能' }))
    const dialog = await screen.findByRole('dialog', { name: '导入技能' })
    const list = await within(dialog).findByRole('list', { name: '本机已有' })
    // Changed here since it came in: marked, and updated after asking, as
    // the library's copy changed since too.
    expect(list).toHaveTextContent('有更新')
    expect(within(list).queryByRole('button', { name: '用这里的内容更新 go-table-tests' })).toBeNull()
    await userEvent.click(within(list).getByRole('button', { name: '用这里的内容更新 notes' }))
    const ask = await screen.findByRole('alertdialog', { name: '用这里的内容更新 notes？' })
    expect(ask).toHaveTextContent('导入后又改过 2 次')
    await userEvent.click(within(ask).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(updated).toEqual(['/home/me/.claude/skills/notes']))
    await waitFor(() => expect(list).not.toHaveTextContent('有更新'))
    // Not known to have changed, and not changed in the library: no asking.
    await userEvent.click(within(list).getByRole('button', { name: '用这里的内容更新 pdf' }))
    await waitFor(() => expect(updated).toEqual(['/home/me/.claude/skills/notes', '/home/me/.agents/skills/pdf']))
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })
})
