import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { LibraryPage } from './LibraryPage'
import { routes } from './library/libraryTesting'

// The skills on this machine in the import (docs/design.md 5.15): two of
// one name in two folders are told apart, and only one can be ticked.
describe('the skills on this machine', () => {
  it('tells skills of one name apart and lets one of them be ticked', async () => {
    stubApi(
      routes({
        '/agents': { agents: [] },
        '/library/local': {
          skills: [
            { name: 'pdf', description: 'Fill in forms.', folder: '/home/me/.claude/skills/pdf', where: '~/.claude/skills' },
            { name: 'pdf', description: 'Read reports.', folder: '/home/me/.agents/skills/pdf', where: '~/.agents/skills' },
            { name: 'notes', description: 'Take notes.', folder: '/home/me/.agents/skills/notes', where: '~/.agents/skills' },
          ],
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    await userEvent.click(await screen.findByRole('button', { name: '导入技能' }))
    const dialog = await screen.findByRole('dialog', { name: '导入技能' })
    const list = await within(dialog).findByRole('list', { name: '本机已有' })
    const [claude, agents] = within(list).getAllByRole('checkbox', { name: /pdf/ })
    expect(list).toHaveTextContent('同名的还有：~/.agents/skills')
    expect(list).toHaveTextContent('同名的还有：~/.claude/skills')
    expect(list).not.toHaveTextContent('notes同名')

    await userEvent.click(claude)
    expect(agents).toBeDisabled()
    expect(list).toHaveTextContent('已勾选 ~/.claude/skills 里的同名技能，只能导入一个')
    await userEvent.click(within(list).getByRole('checkbox', { name: /notes/ }))
    expect(within(dialog).getByRole('button', { name: '导入 2 个技能' })).toBeEnabled()
    // Unticked, the other can be ticked again.
    await userEvent.click(claude)
    expect(agents).toBeEnabled()
  })
})
