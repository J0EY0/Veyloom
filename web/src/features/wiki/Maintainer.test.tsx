import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { UpkeepStatus } from '@/api/types'
import { t } from '@/lib/i18n'
import { stubApi } from '@/test/fetch'
import { project, room, runtimeTraits } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { systemText } from '@/features/threads/systemNote'
import { MaintainerCard } from './Maintainer'

function status(overrides: Partial<UpkeepStatus> = {}): UpkeepStatus {
  return { trigger: 'idle', idle_minutes: 30, waiting: { own: 0, uses: 0, settled: 0 }, queued: false, ...overrides }
}

const keeping = status({
  member_id: 'm1',
  member_name: 'Keeper',
  waiting: { own: 2, uses: 1, settled: 1, people: 3, people_settled: 3 },
  last: {
    id: 'u1',
    member_id: 'm1',
    room_id: 'r1',
    thread_id: 't9',
    machine_id: 'w1',
    status: 'done',
    kind: 'upkeep',
    usage: { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 },
    started_at: '2026-09-22T01:00:00Z',
    ended_at: '2026-09-22T01:02:00Z',
  },
  thread_id: 't9',
})

describe('MaintainerCard', () => {
  it('says who keeps the wiki, how the last upkeep went, what waits, and runs one now', async () => {
    let started = false
    let patched: unknown
    stubApi({
      '/projects': { projects: [{ ...project('p1', 'Veyloom'), wiki_upkeep: true, wiki_maintainer_member_id: 'm1', leader_id: 'm2' }] },
      '/projects/p1/wiki/maintainer': { upkeep: keeping },
      '/rooms/r1/members': {
        members: [
          { id: 'm1', display_name: 'Keeper', enabled: true },
          { id: 'm2', display_name: 'Lead', enabled: true },
        ],
      },
      '/projects/p1/wiki/maintain': () => {
        started = true
        return { upkeep: { ...keeping, queued: true } }
      },
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: project('p1', 'Veyloom'), rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const onOpenThread = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={onOpenThread} />)
    expect(await screen.findByRole('heading', { name: 'Keeper 在维护这个 wiki' })).toBeInTheDocument()
    // Who and how often are changed right here (docs/design.md 5.16): the
    // leader, someone else, or nobody, which turns upkeep off.
    const who = await screen.findByRole('combobox', { name: 'Wiki 维护员' })
    await waitFor(() => expect(who).toHaveTextContent('Keeper'))
    await user.click(who)
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual(['不开', '组长（Lead）', 'Keeper', 'Lead'])
    await user.click(screen.getByRole('option', { name: '组长（Lead）' }))
    await waitFor(() => expect(patched).toEqual({ wiki_maintainer_member_id: '' }))
    await user.click(who)
    await user.click(await screen.findByRole('option', { name: '不开' }))
    await waitFor(() => expect(patched).toEqual({ wiki_upkeep: false }))
    const how = screen.getByRole('combobox', { name: '什么时候整理' })
    expect(how).toHaveTextContent('话题静置 30 分钟后')
    await user.click(how)
    await user.click(await screen.findByRole('option', { name: '每周一次' }))
    await waitFor(() => expect(patched).toEqual({ wiki_maintainer_trigger: 'weekly' }))
    expect(screen.getByText(/上次整理于/)).toBeInTheDocument()
    expect(screen.getByText(/待整理：本群 2 轮，其他项目用本团队技能的 1 轮，人的消息 3 条/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '打开整理话题' }))
    expect(onOpenThread).toHaveBeenCalledWith('t9', 'r1')

    await user.click(screen.getByRole('button', { name: '现在整理' }))
    await waitFor(() => expect(started).toBe(true))
    // Queued now, it cannot be asked for twice.
    expect(await screen.findByText(/等 Keeper 空下来就整理/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '现在整理' })).toBeDisabled()
  })

  it('says when the leader keeps the wiki', async () => {
    stubApi({ '/projects/p1/wiki/maintainer': { upkeep: { ...keeping, leader: true } } })
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={() => {}} />)
    expect(await screen.findByRole('heading', { name: 'Keeper（组长）在维护这个 wiki' })).toBeInTheDocument()
  })

  it('says so when the last upkeep did not finish', async () => {
    stubApi({ '/projects/p1/wiki/maintainer': { upkeep: { ...keeping, last: { ...keeping.last!, status: 'failed' } } } })
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={() => {}} />)
    expect(await screen.findByText(/上次整理没有完成/)).toBeInTheDocument()
  })

  it('offers a maintainer once there is something to go over, the leader unless another is picked, until a person says no', async () => {
    let patched: unknown
    stubApi({
      '/projects': { projects: [{ ...project('p1', 'Veyloom'), leader_id: 'm1' }] },
      '/projects/p1/wiki/maintainer': { upkeep: status({ waiting: { own: 3, uses: 0, settled: 3 } }) },
      '/rooms/r1/members': {
        members: [
          { id: 'm1', display_name: 'Keeper', enabled: true },
          { id: 'm2', display_name: 'Other', enabled: true },
        ],
      },
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: project('p1', 'Veyloom'), rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={() => {}} />)
    expect(await screen.findByRole('heading', { name: '让一个成员来维护这个 wiki？' })).toBeInTheDocument()
    const who = screen.getByRole('combobox', { name: '谁来整理' })
    await waitFor(() => expect(who).toHaveTextContent('组长（Keeper）'))
    const enable = screen.getByRole('button', { name: '开启（每天一次）' })
    await user.click(enable)
    await waitFor(() => expect(patched).toEqual({ wiki_upkeep: true, wiki_maintainer_member_id: '', wiki_maintainer_trigger: 'daily' }))
    await user.click(who)
    await user.click(await screen.findByRole('option', { name: 'Other' }))
    await user.click(enable)
    await waitFor(() => expect(patched).toEqual({ wiki_upkeep: true, wiki_maintainer_member_id: 'm2', wiki_maintainer_trigger: 'daily' }))
  })

  it('warns that a read-only Codex member would write no wiki', async () => {
    stubApi({
      '/projects': { projects: [{ ...project('p1', 'Veyloom'), leader_id: 'm2' }] },
      '/projects/p1/wiki/maintainer': { upkeep: status({ waiting: { own: 1, uses: 0, settled: 1 } }) },
      '/rooms/r1/members': {
        members: [
          { id: 'm1', agent_id: 'a1', display_name: 'Codex', enabled: true, permission_preset: '' },
          { id: 'm2', agent_id: 'a2', display_name: 'Claude', enabled: true, permission_preset: '' },
        ],
      },
      '/agents': {
        agents: [
          { id: 'a1', runtime: 'codex', permission_preset: 'read_only' },
          { id: 'a2', runtime: 'claude', permission_preset: 'read_only' },
        ],
      },
      '/runtime-traits': runtimeTraits,
    })
    const user = userEvent.setup()
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={() => {}} />)
    // The leader, a Claude, writes it.
    await waitFor(() => expect(screen.getByRole('combobox', { name: '谁来整理' })).toHaveTextContent('组长（Claude）'))
    expect(screen.queryByText(/只读的 Codex/)).toBeNull()
    await user.click(screen.getByRole('combobox', { name: '谁来整理' }))
    await user.click(await screen.findByRole('option', { name: 'Codex' }))
    expect(await screen.findByText(/只读的 Codex/)).toBeInTheDocument()
  })

  it('stops offering once declined, and offers nothing when nothing waits', async () => {
    let patched: unknown
    stubApi({
      '/projects': { projects: [project('p1', 'Veyloom'), project('p2', 'Other')] },
      '/projects/p1/wiki/maintainer': { upkeep: status({ waiting: { own: 3, uses: 0, settled: 3 } }) },
      '/projects/p2/wiki/maintainer': { upkeep: status() },
      '/rooms/r1/members': { members: [{ id: 'm1', display_name: 'Keeper', enabled: true }] },
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { ...project('p1', 'Veyloom'), wiki_offer_declined_at: '2026-09-23T02:00:00Z' }, rooms: [room('r1', 'p1', 'main')] }
      },
    })
    const user = userEvent.setup()
    renderWithProviders(<MaintainerCard projectId="p1" roomId="r1" onOpenThread={() => {}} />)
    await user.click(await screen.findByRole('button', { name: '不开' }))
    // Kept on the project, so the chat's card follows it too.
    await waitFor(() => expect(patched).toEqual({ wiki_offer_declined: true }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: '让一个成员来维护这个 wiki？' })).toBeNull())
    renderWithProviders(<MaintainerCard projectId="p2" roomId="r1" onOpenThread={() => {}} />)
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByRole('heading', { name: '让一个成员来维护这个 wiki？' })).toBeNull()
  })
})

describe('systemText', () => {
  it('puts an upkeep note in the UI’s words', () => {
    expect(systemText(t, "Wiki upkeep by Keeper (topics went quiet): 3 turns of this chat, 1 turn of other projects using this team's skills.")).toBe(
      'Keeper 开始整理 wiki（话题静置了）：本群 3 轮，其他项目用本团队技能的 1 轮',
    )
    expect(systemText(t, "Wiki upkeep by Keeper (asked by a person): 0 turns of this chat, 0 turns of other projects using this team's skills.")).toBe(
      'Keeper 开始整理 wiki（有人让整理）：本群 0 轮，其他项目用本团队技能的 0 轮',
    )
    // Later ones say what people said, and the reasons that came with the
    // longer gaps (docs/design.md 5.16).
    expect(
      systemText(t, "Wiki upkeep by Keeper (weekly): 2 turns of this chat, 0 turns of other projects using this team's skills, 5 messages from people."),
    ).toBe('Keeper 开始整理 wiki（每周一次）：本群 2 轮，其他项目用本团队技能的 0 轮，人的消息 5 条')
    expect(
      systemText(
        t,
        "Wiki upkeep by Keeper (the last upkeep left turns to go over): 20 turns of this chat, 0 turns of other projects using this team's skills, 1 message from people.",
      ),
    ).toBe('Keeper 开始整理 wiki（上次没看完）：本群 20 轮，其他项目用本团队技能的 0 轮，人的消息 1 条')
    // Notes it has no words of its own for are shown as the hub wrote them.
    expect(systemText(t, 'Keeper could not start a turn: no machine')).toBe('Keeper could not start a turn: no machine')
  })
})
