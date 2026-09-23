import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { MemoryPrefs } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { SettingsPage } from './SettingsPage'

// The memory settings in General (docs/design.md 5.19): a switch for memory
// as a whole, one for each memory while it is on, and the global memory
// kept in a dialog.

function stub(prefs: MemoryPrefs, saved: MemoryPrefs[] = []) {
  let current = prefs
  return stubApi({
    '/settings/memory': async (req: Request) => {
      if (req.method === 'PUT') {
        current = (await req.json()) as MemoryPrefs
        saved.push(current)
      }
      return { memory: current }
    },
    '/memory': {
      memory: {
        entries: [
          { text: '回复用中文。', date: '2026-09-20', source: 'jinghao' },
          { text: '提交说明用英文。', date: '2026-09-23', source: 'Claude in topic #4 of Veyloom' },
        ],
        chars: 60,
        budget: 2000,
        hash: 'h1',
      },
    },
    '/memory/history': {
      commits: [
        {
          sha: 'c1',
          author: 'human:jinghao',
          at: new Date().toISOString(),
          subject: 'Changed the personal memory',
          changes: [{ kind: 'Update', path: '/memory.md', title: 'Personal memory', text: 'x' }],
          undoable: true,
        },
      ],
    },
  })
}

const general = () => renderWithProviders(<SettingsPage />, { route: '/settings/general', path: '/settings/:section' })

describe('MemorySettings', () => {
  it('shows each memory while memory is on, and saves the switches as a whole', async () => {
    const saved: MemoryPrefs[] = []
    stub({ enabled: true, personal: true, project: true }, saved)
    general()
    const group = await screen.findByRole('region', { name: '记忆' })
    await within(group).findByRole('switch', { name: '全局记忆' })
    expect(within(group).getByText('所有项目都用 · 2 条')).toBeInTheDocument()
    expect(within(group).getByText('在各项目的 Wiki 里看和改')).toBeInTheDocument()

    await userEvent.click(within(group).getByRole('switch', { name: '项目记忆' }))
    await waitFor(() => expect(saved).toEqual([{ enabled: true, personal: true, project: false }]))
    await userEvent.click(within(group).getByRole('switch', { name: '启用记忆' }))
    await waitFor(() => expect(saved.at(-1)).toEqual({ enabled: false, personal: true, project: false }))
    // Off as a whole: the two under it go, and come back as they were.
    expect(within(group).queryByRole('switch', { name: '全局记忆' })).toBeNull()
    await userEvent.click(within(group).getByRole('switch', { name: '启用记忆' }))
    expect(await within(group).findByRole('switch', { name: '项目记忆' })).not.toBeChecked()
  })

  it('keeps the global memory in a dialog, its changes behind the menu', async () => {
    stub({ enabled: true, personal: true, project: true })
    const { router } = general()
    await userEvent.click(await screen.findByRole('button', { name: '管理' }))
    expect(router.state.location.search).toBe('?memory=personal')
    const dialog = await screen.findByRole('dialog', { name: '全局记忆' })
    expect(await within(dialog).findByText('9月23日 · Claude · Veyloom 的话题 #4')).toBeInTheDocument()
    expect(within(dialog).getByText('2 条 · 刚刚更新')).toBeInTheDocument()
    expect(within(dialog).getByRole('textbox', { name: '新的一条' })).toBeInTheDocument()

    await userEvent.click(within(dialog).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '改动记录' }))
    expect(await within(dialog).findByRole('button', { name: '撤回' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('textbox', { name: '新的一条' })).toBeNull()

    await userEvent.click(within(dialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })
})
