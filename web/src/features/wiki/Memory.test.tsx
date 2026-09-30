import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { MemoryView } from '@/api/types'
import { personalSpace } from '@/api/wiki'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { MemoryEditor } from './Memory'

const memory: MemoryView = {
  entries: [
    { text: '回复用中文。', date: '2026-09-20', source: 'alice' },
    { text: '提交说明用英文。', date: '2026-09-23', source: 'Claude in topic #4 of Veyloom' },
  ],
  chars: 60,
  budget: 2000,
  hash: 'h1',
}

// memoryApi serves the personal memory, keeping what is saved.
function memoryApi(saved: { entries: string[]; hash: string }[]) {
  let current = memory
  return stubApi({
    '/memory': async (req: Request) => {
      if (req.method !== 'PUT') return { memory: current }
      const body = (await req.json()) as { entries: string[]; hash: string }
      saved.push(body)
      current = { ...current, entries: body.entries.map((text) => ({ text })), hash: `h${saved.length + 1}` }
      return { memory: current }
    },
    '/memory/history': {
      commits: [
        {
          sha: 'c1',
          author: 'human:alice',
          at: '2026-09-23T03:00:00Z',
          subject: 'Changed the personal memory',
          changes: [{ kind: 'Update', path: '/memory.md', title: 'Personal memory', text: 'x' }],
          undoable: true,
        },
      ],
    },
  })
}

function renderMemory() {
  return renderWithProviders(<MemoryEditor space={personalSpace} onOpenThread={vi.fn()} />)
}

describe('MemoryEditor', () => {
  it('lists the entries with the day each was noted and who noted it', async () => {
    memoryApi([])
    renderMemory()
    const list = await screen.findByRole('list', { name: '条目' })
    expect(within(list).getByText('回复用中文。')).toBeInTheDocument()
    expect(within(list).getByText('9月20日 · alice')).toBeInTheDocument()
    expect(within(list).getByText('9月23日 · Claude · Veyloom 的话题 #4')).toBeInTheDocument()
    // How full it is is said once that matters, not before.
    expect(screen.queryByText('60 / 2000 字')).toBeNull()
    expect(screen.queryByText(/快满了/)).toBeNull()
  })

  it('adds an entry, saved at once from what was read', async () => {
    const saved: { entries: string[]; hash: string }[] = []
    memoryApi(saved)
    renderMemory()
    await userEvent.type(await screen.findByRole('textbox', { name: '新的一条' }), '小步提交。{Enter}')
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toEqual({ entries: ['回复用中文。', '提交说明用英文。', '小步提交。'], hash: 'h1' })
    expect(await screen.findByRole('textbox', { name: '新的一条' })).toHaveValue('')
  })

  it('changes an entry in place, and takes one out', async () => {
    const saved: { entries: string[]; hash: string }[] = []
    memoryApi(saved)
    renderMemory()
    await userEvent.click((await screen.findAllByRole('button', { name: '修改' }))[0])
    const box = screen.getByRole('textbox', { name: '修改' })
    await userEvent.clear(box)
    await userEvent.type(box, '回复一律用中文。{Enter}')
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0].entries).toEqual(['回复一律用中文。', '提交说明用英文。'])

    await userEvent.click((await screen.findAllByRole('button', { name: '删除' }))[1])
    await waitFor(() => expect(saved).toHaveLength(2))
    expect(saved[1]).toEqual({ entries: ['回复一律用中文。'], hash: 'h2' })
  })

  it('with nothing kept, says so in one line, not in an empty list', async () => {
    stubApi({ '/memory': { memory: { ...memory, entries: [], chars: 0 } }, '/memory/history': { commits: [] } })
    renderMemory()
    expect(await screen.findByText('还没有记录任何内容。')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: '条目' })).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '新的一条' })).toBeInTheDocument()
  })

  it('says why a save was refused, in words, and warns when nearly full', async () => {
    stubApi({
      '/memory': async (req: Request) =>
        req.method === 'PUT'
          ? Response.json({ error: 'over', code: 'memoryOverBudget', params: { chars: '2100', budget: '2000' } }, { status: 400 })
          : { memory: { ...memory, chars: 1900 } },
    })
    renderMemory()
    expect(await screen.findByText(/快满了/)).toBeInTheDocument()
    await userEvent.type(screen.getByRole('textbox', { name: '新的一条' }), '再加一条。{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('超出字数上限：保存后会有 2100 字，最多 2000 字。')
  })

  it('shows its changes when asked, each one undoable', async () => {
    memoryApi([])
    renderMemory()
    await userEvent.click(await screen.findByRole('button', { name: '改动记录' }))
    expect(await screen.findByRole('link', { name: '全局记忆' })).toHaveAttribute('href', '/settings/general?memory=personal')
    expect(screen.getByRole('button', { name: '撤回' })).toBeInTheDocument()
  })
})
