import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { Member } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval, turn } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { MemberIsland } from './MemberIsland'

function member(id: string, display_name: string): Member {
  return {
    id,
    room_id: 'r1',
    agent_id: 't',
    machine_id: 'w1',
    display_name,
    repo_path: '',
    branch_mode: 'worktree',
    model: '',
    permission_preset: '',
    enabled: true,
    created_at: '',
  }
}

describe('MemberIsland', () => {
  it('names the working member and opens its topic', async () => {
    const onOpenThread = vi.fn()
    renderWithProviders(
      <MemberIsland
        states={[
          {
            member: member('a1', 'Codex Implementer'),
            status: 'working',
            turn: turn('x1', 't9', { status: 'running', started_at: new Date(Date.now() - 42_000).toISOString(), ended_at: undefined }),
          },
          { member: member('a2', 'Pi Tester'), status: 'idle' },
        ]}
        onOpenThread={onOpenThread}
        onOpenMembers={() => {}}
      />,
    )
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent(/Codex Implementer 正在工作 · 00:4[2-5]/)
    // The clock goes by every second: nothing is read out for it.
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    await userEvent.click(screen.getByRole('button', { name: /Codex Implementer · 工作中/ }))
    expect(onOpenThread).toHaveBeenCalledWith('t9')
  })

  it('sends a question to its card rather than answering it from here', async () => {
    const onOpenThread = vi.fn()
    const question = approval('q1', {
      kind: 'question',
      tool: 'AskUserQuestion',
      thread_id: 't7',
      input: { questions: [{ id: '1', question: 'Which database?', options: [{ label: 'Postgres' }] }] },
    })
    renderWithProviders(
      <MemberIsland
        states={[{ member: member('a1', 'Claude Architect'), status: 'waiting', approval: question }]}
        onOpenThread={onOpenThread}
        onOpenMembers={() => {}}
      />,
    )
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent('Claude Architect 在提问 · Which database?')
    expect(screen.getByLabelText('Claude Architect · 等你回答')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '允许' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '去回答' }))
    expect(onOpenThread).toHaveBeenCalledWith('t7')
  })

  it('counts idle members and opens the members panel', async () => {
    const onOpenMembers = vi.fn()
    renderWithProviders(
      <MemberIsland
        states={[
          { member: member('a1', 'Codex Implementer'), status: 'idle' },
          { member: member('a2', 'Pi Tester'), status: 'offline' },
        ]}
        onOpenThread={() => {}}
        onOpenMembers={onOpenMembers}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: '2 个成员空闲' }))
    expect(onOpenMembers).toHaveBeenCalled()
    expect(screen.getByLabelText('Pi Tester · 机器离线')).toBeInTheDocument()
  })

  it('shows the faces alone in a bar with no room for the count, but says what is going on', () => {
    const idle = [
      { member: member('a1', 'Codex Implementer'), status: 'idle' as const },
      { member: member('a2', 'Pi Tester'), status: 'offline' as const },
    ]
    const { unmount } = renderWithProviders(<MemberIsland states={idle} onOpenThread={() => {}} onOpenMembers={() => {}} facesOnly />)
    expect(screen.queryByRole('button', { name: '2 个成员空闲' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Pi Tester · 机器离线')).toBeInTheDocument()
    unmount()

    // Only the count gives way: a member at work is still told.
    renderWithProviders(
      <MemberIsland
        states={[{ ...idle[0], status: 'working', turn: turn('x1', 't9', { status: 'running', ended_at: undefined }) }, idle[1]]}
        onOpenThread={() => {}}
        onOpenMembers={() => {}}
        facesOnly
      />,
    )
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent('Codex Implementer 正在工作')
  })
})

describe('MemberIsland quiet', () => {
  // A turn that showed no sign of life for a while goes before the others
  // at work, and a person may cancel it, with a new session or without
  // (docs/design.md 5.23.8).
  it('puts a quiet turn first, with cancel and cancel with a new session', async () => {
    const bodies: unknown[] = []
    stubApi({ '/turns/x2/cancel': async (req: Request) => (bodies.push(await req.json()), new Response(null, { status: 202 })) })
    const quiet = new Date(Date.now() - 12 * 60_000 - 5_000).toISOString()
    renderWithProviders(
      <MemberIsland
        states={[
          { member: member('a1', 'Busy'), status: 'working', turn: turn('x1', 't1', { status: 'running', ended_at: undefined }) },
          { member: member('a2', 'Stuck'), status: 'working', turn: turn('x2', 't2', { status: 'running', ended_at: undefined, quiet_since: quiet }) },
        ]}
        onOpenThread={vi.fn()}
        onOpenMembers={vi.fn()}
      />,
    )
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent('Stuck · 12 分钟没有任何进展')
    expect(screen.getByRole('button', { name: /Stuck · 可能卡住/ })).toBeInTheDocument()
    const fresh = screen.getByRole('button', { name: '取消并开启新会话' })
    expect(fresh).toHaveTextContent('取消并开启新会话')
    await userEvent.click(screen.getByRole('button', { name: '取消' }))
    await userEvent.click(fresh)
    await waitFor(() => expect(bodies).toEqual([{}, { new_session: true }]))
  })

  it('says the short of the second way out where it is short of room, which its full name holds', () => {
    const quiet = new Date(Date.now() - 12 * 60_000).toISOString()
    renderWithProviders(
      <MemberIsland
        states={[{ member: member('a2', 'Stuck'), status: 'working', turn: turn('x2', 't2', { status: 'running', ended_at: undefined, quiet_since: quiet }) }]}
        onOpenThread={vi.fn()}
        onOpenMembers={vi.fn()}
        terse
      />,
    )
    const fresh = screen.getByRole('button', { name: '取消并开启新会话' })
    expect(fresh).toHaveTextContent(/^开启新会话$/)
  })
})

describe('MemberIsland waiting', () => {
  it('says why a paused member waits, and resumes it', async () => {
    const at = new Date(Date.now() + 3_600_000)
    let lifted = ''
    stubApi({ '/pauses/p1': (req: Request) => ((lifted = req.method), new Response(null, { status: 204 })) })
    renderWithProviders(
      <MemberIsland
        states={[
          {
            member: member('a1', 'Slow'),
            status: 'paused',
            pause: { id: 'p1', machine_id: 'w1', runtime: 'claude', reason: 'quota', detail: '', ends_at: at.toISOString(), created_at: '' },
          },
          { member: member('a2', 'Pi Tester'), status: 'idle' },
        ]}
        onOpenThread={vi.fn()}
        onOpenMembers={vi.fn()}
      />,
    )
    const hhmm = `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent(`Slow · 额度用完 · ${hhmm} 恢复`)
    await userEvent.click(screen.getByRole('button', { name: '继续' }))
    await waitFor(() => expect(lifted).toBe('DELETE'))
  })

  it('puts the waiting member first with the two buttons', async () => {
    setCurrentUser({ id: 'u1', name: 'alice' })
    let posted: unknown
    stubApi({
      '/approvals/ap1/decide': async (req) => {
        posted = await req.json()
        return { approval: approval('ap1', { status: 'allowed', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(
      <MemberIsland
        states={[
          { member: member('a2', 'Pi Tester'), status: 'working', turn: turn('x2', 't2', { status: 'running', ended_at: undefined }) },
          {
            member: member('a1', 'Codex Implementer'),
            status: 'waiting',
            turn: turn('x1', 't1', { status: 'running', ended_at: undefined }),
            approval: approval('ap1'),
          },
        ]}
        onOpenThread={() => {}}
        onOpenMembers={() => {}}
      />,
    )
    expect(screen.getByRole('group', { name: '成员状态' })).toHaveTextContent('Codex Implementer 在等你审批 · make test')
    // A person waited for is said as it happens.
    expect(screen.getByRole('status')).toHaveTextContent('Codex Implementer 在等你审批 · make test')
    await userEvent.click(screen.getByRole('button', { name: '允许' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: true, message: '' }))
  })
})
