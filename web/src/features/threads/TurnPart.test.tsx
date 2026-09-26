import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyTurnEvent, resetLiveTurns } from '@/lib/liveTurns'
import { stubApi } from '@/test/fetch'
import { approval, message, turn } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { TurnPart } from './TurnPart'

const who = { name: 'Codex Implementer', kind: 'agent' as const, runtime: 'codex' }

describe('TurnPart', () => {
  beforeEach(() => resetLiveTurns())

  it('streams a running turn under its face: one line for the tools, then the words arriving', async () => {
    stubApi({ '/turns/x1/approvals': { approvals: [] } })
    applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: '{"command":"go test ./..."}' })
    applyTurnEvent('x1', { kind: 'tool_result', tool: 'Bash', text: 'ok' })
    applyTurnEvent('x1', { kind: 'text', text: '都过了' })
    renderWithProviders(<TurnPart turn={turn('x1', 't1', { status: 'running', ended_at: undefined })} messages={[]} who={who} first last />)

    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
    expect(screen.queryByText(/第 \d+ 轮/)).not.toBeInTheDocument()
    // The tools fold into one line; while the turn runs its latest steps
    // show anyway.
    expect(screen.getByText('go test ./...')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '过程 · 跑了 1 条命令' }))
    expect(screen.getByText('go test ./...')).toBeInTheDocument()
    expect(screen.getByText('都过了')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeInTheDocument()
  })

  it('reads a finished turn from its transcript, says how long it took and opens its record', async () => {
    const onOpenTurn = vi.fn()
    stubApi({
      '/turns/x1/approvals': { approvals: [] },
      '/turns/x1/transcript': new Response('{"kind":"event","event":{"kind":"file_changed","path":"internal/hub/events_test.go"}}\n'),
    })
    const said = message('m2', 2, { member_id: 'a1', user_id: undefined, body: '补好了。', turn_id: 'x1' })
    renderWithProviders(<TurnPart turn={turn('x1', 't1')} messages={[said]} who={who} first last onOpenTurn={onOpenTurn} />)

    expect(await screen.findByText('补好了。')).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: '过程 · 改了 1 个文件 · 42 秒' }))
    expect(screen.getByText('internal/hub/events_test.go')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '取消' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '查看完整记录' }))
    expect(onOpenTurn).toHaveBeenCalledWith('x1')
  })

  it('says what a turn waits for rather than seeming to type', async () => {
    stubApi({
      '/turns/x1/approvals': {
        approvals: [approval('q1', { turn_id: 'x1', kind: 'question', tool: 'AskUserQuestion', input: { questions: [{ id: '1', question: 'Which?' }] } })],
      },
    })
    renderWithProviders(<TurnPart turn={turn('x1', 't1', { status: 'running', ended_at: undefined })} messages={[]} who={who} first last />)
    expect(await screen.findByText('在等你回答')).toBeInTheDocument()
    expect(screen.queryByText('正在输入')).not.toBeInTheDocument()
  })

  it("keeps a running turn's latest steps in sight, the earlier ones a press away", async () => {
    stubApi({ '/turns/x1/approvals': { approvals: [] } })
    for (const command of ['git log --oneline -5', 'git show veyloom/coder:store.go', 'ls -la', 'git merge veyloom/coder', 'go test ./... -v']) {
      applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: JSON.stringify({ command }) })
      applyTurnEvent('x1', { kind: 'tool_result', tool: 'Bash', text: 'ok' })
    }
    renderWithProviders(<TurnPart turn={turn('x1', 't1', { status: 'running', ended_at: undefined })} messages={[]} who={who} first last />)
    expect(screen.getByText('ls -la')).toBeInTheDocument()
    expect(screen.getByText('go test ./... -v')).toBeInTheDocument()
    expect(screen.queryByText('git log --oneline -5')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '前面还有 2 步' }))
    expect(screen.getByText('git log --oneline -5')).toBeInTheDocument()
  })

  it('keeps what the runtime told people in sight, running or finished', async () => {
    stubApi({
      '/turns/x1/approvals': { approvals: [] },
      '/turns/x2/approvals': { approvals: [] },
      '/turns/x2/transcript': new Response(
        '{"kind":"event","event":{"kind":"notice","level":"error","text":"Codex asked for item/x, which Veyloom cannot answer yet"}}\n',
      ),
    })
    applyTurnEvent('x1', { kind: 'notice', level: 'warning', text: 'MCP server node_repl failed to start' })
    applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: '{"command":"make"}' })
    const { unmount } = renderWithProviders(<TurnPart turn={turn('x1', 't1', { status: 'running', ended_at: undefined })} messages={[]} who={who} first last />)
    // Not folded into the activity line: there without opening anything.
    expect(screen.getByRole('note')).toHaveTextContent('警告: MCP server node_repl failed to start')
    expect(screen.getByRole('button', { name: '过程 · 跑了 1 条命令' })).toBeInTheDocument()
    unmount()

    renderWithProviders(<TurnPart turn={turn('x2', 't1')} messages={[]} who={who} first last />)
    expect(await screen.findByRole('note')).toHaveTextContent('错误: Codex asked for item/x, which Veyloom cannot answer yet')
  })

  it('leaves the tools to the first stretch of a turn and the words arriving to the last', () => {
    stubApi({ '/turns/x1/approvals': { approvals: [] } })
    applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: '{"command":"make test"}' })
    applyTurnEvent('x1', { kind: 'text', text: '还在跑' })
    const running = turn('x1', 't1', { status: 'running', ended_at: undefined })
    const { unmount } = renderWithProviders(<TurnPart turn={running} messages={[]} who={who} first last={false} />)
    expect(screen.getByRole('button', { name: '过程 · 跑了 1 条命令' })).toBeInTheDocument()
    expect(screen.queryByText('还在跑')).not.toBeInTheDocument()
    unmount()

    renderWithProviders(<TurnPart turn={running} messages={[]} who={who} first={false} last />)
    expect(screen.queryByRole('button', { name: '过程 · 跑了 1 条命令' })).not.toBeInTheDocument()
    expect(screen.getByText('还在跑')).toBeInTheDocument()
  })

  it('says which member woke a turn', () => {
    stubApi({ '/turns/x2/approvals': { approvals: [] }, '/turns/x2/transcript': new Response('') })
    const said = message('m3', 3, { member_id: 'a2', user_id: undefined, body: 'On it.', turn_id: 'x2' })
    renderWithProviders(<TurnPart turn={turn('x2', 't1', { woken_by_turn_id: 'x1' })} messages={[said]} who={who} first last wokenBy="Lead" />)
    expect(screen.getByText('由 Lead 叫醒')).toBeInTheDocument()
  })
})
