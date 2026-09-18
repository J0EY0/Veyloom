import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { AgentMarkdown, withMentionTags } from './agent-markdown'

const names = new Map([
  ['a1', 'Codex Implementer'],
  ['u1', 'alice'],
])

describe('withMentionTags', () => {
  it('wraps mentioned names in a tag, longest first', () => {
    const wide = new Map([...names, ['a2', 'Codex Implementer B']])
    const out = withMentionTags(
      '@Codex Implementer B 和 @Codex Implementer 看看',
      [
        { kind: 'agent', id: 'a1' },
        { kind: 'agent', id: 'a2' },
      ],
      wide,
    )
    expect(out).toBe('<mention kind="agent" id="a2">@Codex Implementer B</mention> 和 <mention kind="agent" id="a1">@Codex Implementer</mention> 看看')
  })

  it('leaves text alone without mentions', () => {
    expect(withMentionTags('plain @nobody', null, names)).toBe('plain @nobody')
  })
})

describe('AgentMarkdown', () => {
  it('drops the caret once the text stops arriving, though the text is the same', () => {
    const caret = (root: HTMLElement) => root.querySelector('[class*="streamdown-caret"]')
    const { container, rerender } = render(<AgentMarkdown text="好" mentions={null} names={names} streaming />)
    expect(caret(container)).not.toBeNull()
    // The finished message says what the stream last said.
    rerender(<AgentMarkdown text="好" mentions={null} names={names} />)
    expect(caret(container)).toBeNull()
  })

  it('renders markdown, and an agent mention as a take-over button', async () => {
    const onTakeOver = vi.fn()
    render(
      <AgentMarkdown
        text={'**好**，交给 @Codex Implementer 跑一遍，@alice 看结果。'}
        mentions={[
          { kind: 'agent', id: 'a1' },
          { kind: 'user', id: 'u1' },
        ]}
        names={names}
        onTakeOver={onTakeOver}
      />,
    )
    expect(await screen.findByText('好')).toBeInTheDocument()
    // The pill says what it does in its own words, with nothing on hover.
    const takeOver = screen.getByRole('button', { name: /Codex Implementer/ })
    await userEvent.hover(takeOver)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    await userEvent.click(takeOver)
    expect(onTakeOver).toHaveBeenCalledWith({ kind: 'agent', id: 'a1' }, 'Codex Implementer')
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /alice/ })).not.toBeInTheDocument()
  })
})
