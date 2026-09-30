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
          `{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"pi","spec":{"prompt":"${brief}","system_prompt":"You implement what was designed.\\n\\nHow this team chat works."}}`,
          `{"kind":"restart","at":"2026-09-14T02:00:05Z","error":"session not found","spec":{"prompt":"${retried}"}}`,
          '{"kind":"done","at":"2026-09-14T02:00:42Z"}',
        ].join('\n'),
        { headers: { 'Content-Type': 'application/x-ndjson' } },
      ),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x4" onClose={() => {}} />)

    const first = await screen.findByRole('button', { name: '简报 · 4 行' })
    expect(screen.getByText('开始 · Pi')).toBeInTheDocument()
    expect(screen.getByText(/会话无法续接，已换用新会话重新运行/)).toBeInTheDocument()
    expect(screen.queryByText(/This topic, #1, in full/)).not.toBeInTheDocument()

    await userEvent.click(first)
    expect(screen.getByText(/This topic, #1, in full/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '收起简报' })).toBeInTheDocument()
    // The second run's brief stays folded.
    expect(screen.getByRole('button', { name: '简报 · 2 行' })).toBeInTheDocument()
    expect(screen.queryByText(/This is a new session/)).not.toBeInTheDocument()

    // And what the run was given as its system prompt.
    expect(screen.queryByText(/How this team chat works/)).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '系统提示 · 3 行' }))
    expect(screen.getByText(/How this team chat works/)).toBeInTheDocument()
  })

  it('shows what the turn was passed as it ran, folded, and what missed it', async () => {
    const passed = 'New in topic #1 while you were at work on it:\\n>> [alice] use the new grammar\\n'
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x5': { turn: turn('x5', 't1', { error: '' }) },
      '/turns/x5/transcript': new Response(
        [
          '{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"claude"}',
          `{"kind":"event","at":"2026-09-14T02:00:03Z","event":{"kind":"steer","text":"${passed}","steer_id":"s1","seq":1}}`,
          '{"kind":"event","at":"2026-09-14T02:00:04Z","event":{"kind":"steer_dropped","steer_id":"s2","seq":2}}',
          '{"kind":"event","at":"2026-09-14T02:00:05Z","event":{"kind":"quota","quota":{"window":"5h","used_percent":91},"seq":3}}',
          '{"kind":"done","at":"2026-09-14T02:00:42Z"}',
        ].join('\n'),
        { headers: { 'Content-Type': 'application/x-ndjson' } },
      ),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x5" onClose={() => {}} />)

    expect(await screen.findByText('插话 · 话题中的新消息已发送给这一轮')).toBeInTheDocument()
    expect(screen.getByText('插话没赶上这一轮，将留到下一轮')).toBeInTheDocument()
    // How the account stood, as the runtime told (docs/design.md 5.23.3).
    expect(screen.getByText('5 小时额度已用 91%')).toBeInTheDocument()
    expect(screen.queryByText(/use the new grammar/)).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '发送的内容 · 2 行' }))
    expect(screen.getByText(/>> \[alice\] use the new grammar/)).toBeInTheDocument()
  })

  it('shows that a turn that said nothing was asked for its reply, and what it was asked', async () => {
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x6': { turn: turn('x6', 't1', { error: '' }) },
      '/turns/x6/transcript': new Response(
        [
          '{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"codex"}',
          '{"kind":"reply_asked","at":"2026-09-14T02:00:20Z","runtime":"codex","spec":{"prompt":"Your turn ended without a word in this topic. Reply now."}}',
          '{"kind":"event","at":"2026-09-14T02:00:21Z","event":{"kind":"text","text":"Nothing needed changing."}}',
          '{"kind":"done","at":"2026-09-14T02:00:42Z"}',
        ].join('\n'),
        { headers: { 'Content-Type': 'application/x-ndjson' } },
      ),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x6" onClose={() => {}} />)

    expect(await screen.findByText('没有在话题中留下回复，已在同一会话中请它补充')).toBeInTheDocument()
    expect(screen.getByText('Nothing needed changing.')).toBeInTheDocument()
    expect(screen.queryByText(/Reply now/)).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '追问内容 · 1 行' }))
    expect(screen.getByText(/Reply now/)).toBeInTheDocument()
  })

  it('says how long a running turn showed no sign of life, and cancels it with a new session', async () => {
    const bodies: unknown[] = []
    const since = new Date(Date.now() - 12 * 60_000 - 5_000).toISOString()
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x7': { turn: turn('x7', 't1', { status: 'running', ended_at: undefined, error: '', quiet_since: since }) },
      '/turns/x7/transcript': new Response('{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"codex"}\n', {
        headers: { 'Content-Type': 'application/x-ndjson', 'X-Transcript-Complete': 'false' },
      }),
      '/turns/x7/cancel': async (req: Request) => (bodies.push(await req.json()), new Response(null, { status: 202 })),
    })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x7" onClose={() => {}} />)

    expect(await screen.findByText('12 分钟没有任何进展')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '取消并开启新会话' }))
    await waitFor(() => expect(bodies).toEqual([{ new_session: true }]))
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
    applyTurnEvent('x2', { kind: 'tool_call', tool: 'read_file', input: 'README.md', at: '2026-09-14T02:00:01Z', seq: 1 })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x2" onClose={() => {}} />)

    // Still getting ready, it has no transcript yet: what was heard shows.
    expect(await screen.findByText('read_file README.md')).toBeInTheDocument()
    await waitFor(() => expect(calls).toContain('GET /turns/x2/transcript'))
    expect(screen.queryByText(/token/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '取消轮次' }))
    await waitFor(() => expect(calls).toContain('POST /turns/x2/cancel'))
  })

  it('reads a running turn as far as it is written, and what came after live', async () => {
    const written = [
      '{"kind":"start","at":"2026-09-14T02:00:00Z","runtime":"claude","spec":{"prompt":"You are Coder.\\n>> [alice] fix it"}}',
      '{"kind":"event","at":"2026-09-14T02:00:00Z","event":{"kind":"session","session_ref":"s-1","seq":1}}',
      '{"kind":"event","at":"2026-09-14T02:00:01Z","event":{"kind":"tool_call","tool":"Read","input":"{\\"file_path\\":\\"a.go\\"}","call_id":"c1","seq":2}}',
      '{"kind":"event","at":"2026-09-14T02:00:02Z","event":{"kind":"tool_result","tool":"Read","text":"package a","call_id":"c1","seq":3}}',
      '{"kind":"event","at":"2026-09-14T02:00:03Z","event":{"kind":"text","text":"我先","seq":4}}',
    ].join('\n')
    stubApi({
      '/rooms/r1/members': members,
      '/turns/x5': { turn: turn('x5', 't1', { status: 'running', ended_at: undefined }) },
      '/turns/x5/transcript': () => new Response(written, { headers: { 'Content-Type': 'application/x-ndjson' } }),
    })
    // Opened late, the page heard the turn from event 4 on.
    applyTurnEvent('x5', { kind: 'text', text: '我先', at: '2026-09-14T02:00:03Z', seq: 4 })
    applyTurnEvent('x5', { kind: 'text', text: '跑测试。', at: '2026-09-14T02:00:04Z', seq: 5 })
    applyTurnEvent('x5', { kind: 'tool_call', tool: 'Bash', input: '{"command":"go test"}', at: '2026-09-14T02:00:05Z', seq: 6 })
    renderWithProviders(<TurnDrawer roomId="r1" turnId="x5" onClose={() => {}} />)

    // The start, with its brief, and the read from before the page listened.
    expect(await screen.findByRole('button', { name: '简报 · 2 行' })).toBeInTheDocument()
    expect(screen.getByText('开始 · Claude Code')).toBeInTheDocument()
    expect(screen.getByText('package a')).toBeInTheDocument()
    // The words said in pieces, heard both ways, are one line, once.
    expect(screen.getByText('我先跑测试。')).toBeInTheDocument()
    expect(screen.getByText('go test')).toBeInTheDocument()
    // Start, the read and its result, the words, the command.
    expect(screen.getAllByRole('listitem')).toHaveLength(5)
  })
})
