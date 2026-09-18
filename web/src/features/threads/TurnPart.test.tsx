import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyTurnEvent, resetLiveTurns } from '@/lib/liveTurns'
import { stubApi } from '@/test/fetch'
import { message, turn } from '@/test/fixtures'
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
    expect(screen.getByText('Codex')).toBeInTheDocument()
    expect(screen.queryByText(/第 \d+ 轮/)).not.toBeInTheDocument()
    // The tools fold into one line until opened.
    expect(screen.queryByText('go test ./...')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '1 个工具调用' }))
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
    await userEvent.click(await screen.findByRole('button', { name: '改了 1 个文件 · 42 秒' }))
    expect(screen.getByText('internal/hub/events_test.go')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '取消' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '查看完整记录' }))
    expect(onOpenTurn).toHaveBeenCalledWith('x1')
  })

  it('leaves the tools to the first stretch of a turn and the words arriving to the last', () => {
    stubApi({ '/turns/x1/approvals': { approvals: [] } })
    applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: '{"command":"make test"}' })
    applyTurnEvent('x1', { kind: 'text', text: '还在跑' })
    const running = turn('x1', 't1', { status: 'running', ended_at: undefined })
    const { unmount } = renderWithProviders(<TurnPart turn={running} messages={[]} who={who} first last={false} />)
    expect(screen.getByRole('button', { name: '1 个工具调用' })).toBeInTheDocument()
    expect(screen.queryByText('还在跑')).not.toBeInTheDocument()
    unmount()

    renderWithProviders(<TurnPart turn={running} messages={[]} who={who} first={false} last />)
    expect(screen.queryByRole('button', { name: '1 个工具调用' })).not.toBeInTheDocument()
    expect(screen.getByText('还在跑')).toBeInTheDocument()
  })
})
