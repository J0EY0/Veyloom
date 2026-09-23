import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { applyTurnEvent, resetLiveTurns } from '@/lib/liveTurns'
import { stubApi } from '@/test/fetch'
import { turn } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { TurnDrawer } from './TurnDrawer'

const members = { members: [{ id: 'a1', display_name: 'Codex Implementer' }] }

describe('TurnDrawer', () => {
  beforeEach(() => resetLiveTurns())

  it('reads a finished turn from its transcript', async () => {
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x1': { turn: turn('x1', 't1', { error: '' }) },
      '/turns/x1/transcript': new Response(
        [
          '{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"fake"}',
          '{"kind":"event","at":"2026-09-14T02:00:00Z","event":{"kind":"session","session_ref":"s-1"}}',
          '{"kind":"event","at":"2026-09-14T02:00:01Z","event":{"kind":"tool_call","tool":"Bash","input":"{\\"command\\":\\"go test\\"}"}}',
          '{"kind":"event","at":"2026-09-14T02:00:02Z","event":{"kind":"tool_result","tool":"Bash","text":"ok"}}',
          '{"kind":"done","at":"2026-09-14T02:00:42Z"}',
        ].join('\n'),
        { headers: { 'Content-Type': 'application/x-ndjson' } },
      ),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x1" onClose={() => {}} />)

    expect(await screen.findByRole('heading', { name: '轮次详情' })).toBeInTheDocument()
    expect(await screen.findByText('go test')).toBeInTheDocument()
    expect(screen.getByText('开始 · fake')).toBeInTheDocument()
    // The session record is left out: start, the call, its result, the end.
    expect(screen.getAllByRole('listitem')).toHaveLength(4)
    expect(screen.getByText('用时 42 秒')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '取消轮次' })).not.toBeInTheDocument()
    // The runtime reported no tokens: no count at all rather than a zero.
    expect(screen.queryByText(/token/)).not.toBeInTheDocument()
  })

  it('shows the brief each run began with, folded until asked for', async () => {
    const brief = 'You are \\"Codex Implementer\\".\\n\\nThis topic, #1, in full:\\n>> [alice] fix it'
    const retried = 'This is a new session.\\n>> [alice] fix it'
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x4': { turn: turn('x4', 't1', { error: '' }) },
      '/turns/x4/transcript': new Response(
        [
          `{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"pi","spec":{"prompt":"${brief}"}}`,
          `{"kind":"restart","at":"2026-09-14T02:00:05Z","error":"session not found","spec":{"prompt":"${retried}"}}`,
          '{"kind":"done","at":"2026-09-14T02:00:42Z"}',
        ].join('\n'),
        { headers: { 'Content-Type': 'application/x-ndjson' } },
      ),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x4" onClose={() => {}} />)

    const first = await screen.findByRole('button', { name: '简报 · 4 行' })
    expect(screen.getByText('开始 · Pi')).toBeInTheDocument()
    expect(screen.getByText(/会话续不上，换新会话重跑/)).toBeInTheDocument()
    expect(screen.queryByText(/This topic, #1, in full/)).not.toBeInTheDocument()

    await userEvent.click(first)
    expect(screen.getByText(/This topic, #1, in full/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '收起简报' })).toBeInTheDocument()
    // The second run's brief stays folded.
    expect(screen.getByRole('button', { name: '简报 · 2 行' })).toBeInTheDocument()
    expect(screen.queryByText(/This is a new session/)).not.toBeInTheDocument()
  })

  it('says what a finished turn spent, part by part on hover', async () => {
    const usage = { input_tokens: 1234, cache_read_tokens: 150_000, cache_write_tokens: 0, output_tokens: 890 }
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x3': { turn: turn('x3', 't1', { status: 'failed', error: 'rate limited', usage }) },
      '/turns/x3/transcript': new Response('', { headers: { 'Content-Type': 'application/x-ndjson' } }),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x3" onClose={() => {}} />)

    // A failed turn spent tokens all the same.
    const count = await screen.findByText('15.2万 token')
    await userEvent.hover(count)
    expect(await screen.findByRole('tooltip')).toHaveTextContent('输入 1,234 · 缓存读取 150,000 · 输出 890')
  })

  it('shows live events for a running turn and can cancel it', async () => {
    const calls = stubApi({
      '/rooms/r1/members': members,
      '/turns/x2': { turn: turn('x2', 't1', { status: 'running', ended_at: undefined }) },
      '/turns/x2/cancel': new Response(null, { status: 202 }),
    })
    applyTurnEvent('x2', { kind: 'tool_call', tool: 'read_file', input: 'README.md', at: '2026-09-14T02:00:01Z' })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x2" onClose={() => {}} />)

    expect(await screen.findByText('read_file README.md')).toBeInTheDocument()
    expect(calls).not.toContain('GET /turns/x2/transcript')
    expect(screen.queryByText(/token/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '取消轮次' }))
    await waitFor(() => expect(calls).toContain('POST /turns/x2/cancel'))
  })
})
