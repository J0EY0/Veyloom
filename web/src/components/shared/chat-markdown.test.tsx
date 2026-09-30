import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { asTyped, ChatMarkdown, withMentionTags } from './chat-markdown'

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

  it('keeps a longer name whole though only the shorter is mentioned', () => {
    const wide = new Map([...names, ['a2', 'Codex Implementer B']])
    expect(withMentionTags('@Codex Implementer B 和 @Codex Implementer', [{ kind: 'agent', id: 'a1' }], wide)).toBe(
      '@Codex Implementer B 和 <mention kind="agent" id="a1">@Codex Implementer</mention>',
    )
  })

  it('leaves names in code as written', () => {
    const mentioned = [{ kind: 'user' as const, id: 'u1' }]
    expect(withMentionTags('写 `@alice` 就能叫到，@alice 你看下', mentioned, names)).toBe(
      '写 `@alice` 就能叫到，<mention kind="user" id="u1">@alice</mention> 你看下',
    )
    expect(withMentionTags('```\n@alice run\n```\n@alice', mentioned, names)).toBe('```\n@alice run\n```\n<mention kind="user" id="u1">@alice</mention>')
    // Still arriving, a block not yet closed is code to its end.
    expect(withMentionTags('see:\n```\n@alice', mentioned, names)).toBe('see:\n```\n@alice')
  })
})

describe('asTyped', () => {
  it('ends each typed line in a hard break, and leaves the blank ones', () => {
    expect(asTyped('first\nsecond\n\nthird')).toBe('first  \nsecond  \n\nthird  ')
  })

  it('ends a list or a quote at a plain line, as a person means it', () => {
    expect(asTyped('- work\n- home\nAnd ship it')).toBe('- work  \n- home  \n\nAnd ship it  ')
    expect(asTyped('> they said\nI agree')).toBe('> they said  \n\nI agree  ')
    // An indented line goes on with the item.
    expect(asTyped('- work\n  and more\n- home')).toBe('- work  \n  and more  \n- home  ')
  })

  it('shows angle brackets as typed, outside code alone', () => {
    expect(asTyped('use <Composer> and `<b>`')).toBe('use &lt;Composer> and `<b>`  ')
    // A fenced block, its fences and all, is shown as it is.
    expect(asTyped('```\n<div>\n```')).toBe('```\n<div>\n```')
  })
})

describe('ChatMarkdown', () => {
  it('drops the caret once the text stops arriving, though the text is the same', () => {
    const caret = (root: HTMLElement) => root.querySelector('[class*="streamdown-caret"]')
    const { container, rerender } = render(<ChatMarkdown text="好" mentions={null} names={names} streaming />)
    expect(caret(container)).not.toBeNull()
    // The finished message says what the stream last said.
    rerender(<ChatMarkdown text="好" mentions={null} names={names} />)
    expect(caret(container)).toBeNull()
  })

  it('renders markdown, and an agent mention as a take-over button', async () => {
    const onTakeOver = vi.fn()
    render(
      <ChatMarkdown
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
