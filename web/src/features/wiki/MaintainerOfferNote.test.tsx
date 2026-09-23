import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { UpkeepStatus } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { MaintainerOfferNote } from './MaintainerOfferNote'

const waiting: UpkeepStatus = { trigger: 'daily', idle_minutes: 30, waiting: { own: 3, uses: 0, settled: 3 }, queued: false }
const kept: UpkeepStatus = { ...waiting, member_id: 'm1', member_name: 'Keeper', waiting: { own: 0, uses: 0, settled: 0 } }
const members = { members: [{ id: 'm1', display_name: 'Keeper', enabled: true }] }

// The chat's card offering a wiki maintainer (docs/design.md 5.16).
describe('MaintainerOfferNote', () => {
  it('offers a maintainer, daily, then says who keeps the wiki', async () => {
    let patched: unknown
    stubApi({
      '/projects': { projects: [project('p1', 'Veyloom')] },
      '/projects/p1/wiki/maintainer': () => ({ upkeep: patched ? kept : waiting }),
      '/rooms/r1/members': members,
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return {
          project: { ...project('p1', 'Veyloom'), wiki_maintainer_member_id: 'm1', wiki_maintainer_trigger: 'daily' },
          rooms: [room('r1', 'p1', 'main')],
        }
      },
    })
    const user = userEvent.setup()
    renderWithProviders(<MaintainerOfferNote projectId="p1" roomId="r1" />)
    expect(await screen.findByText('让一个成员来维护这个 wiki？')).toBeInTheDocument()
    expect(await screen.findByText(/本群有 3 轮对话还没整理进 wiki/)).toBeInTheDocument()
    await user.click(screen.getByRole('combobox', { name: '选一个成员' }))
    await user.click(await screen.findByRole('option', { name: 'Keeper' }))
    await user.click(screen.getByRole('button', { name: '开启（每天一次）' }))
    await waitFor(() => expect(patched).toEqual({ wiki_maintainer_member_id: 'm1', wiki_maintainer_trigger: 'daily' }))
    expect(await screen.findByText('Keeper 在维护这个 wiki')).toBeInTheDocument()
    expect(screen.getByText(/每天整理一次/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '不开' })).toBeNull()
  })

  it('keeps a no, and says where to change one’s mind', async () => {
    let patched: unknown
    stubApi({
      '/projects': { projects: [project('p1', 'Veyloom')] },
      '/projects/p1/wiki/maintainer': { upkeep: waiting },
      '/rooms/r1/members': members,
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { ...project('p1', 'Veyloom'), wiki_offer_declined_at: '2026-09-23T02:00:00Z' }, rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    renderWithProviders(<MaintainerOfferNote projectId="p1" roomId="r1" />)
    await user.click(await screen.findByRole('button', { name: '不开' }))
    await waitFor(() => expect(patched).toEqual({ wiki_offer_declined: true }))
    expect(await screen.findByText(/你选择了不开维护员/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '开启（每天一次）' })).toBeNull()
  })
})
