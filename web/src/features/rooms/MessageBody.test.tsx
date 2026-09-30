import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MessageBody } from './MessageBody'

const names = new Map([
  ['a1', 'Codex Implementer'],
  ['a2', 'Codex Implementer B'],
  ['u1', 'alice'],
])

describe('MessageBody', () => {
  it('renders a pill per mention, longest name first', async () => {
    render(
      <MessageBody
        body="@Codex Implementer B 和 @alice 看看"
        mentions={[
          { kind: 'agent', id: 'a2' },
          { kind: 'user', id: 'u1' },
        ]}
        names={names}
      />,
    )
    expect(await screen.findByText('Codex Implementer B')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.queryByText(/@alice/)).not.toBeInTheDocument()
  })

  it('draws what a person wrote as markdown: code and emphasis', async () => {
    const { container } = render(<MessageBody body={'`notes tags` should be **fast**'} mentions={null} names={names} />)
    expect(await screen.findByText('notes tags')).toHaveAttribute('data-streamdown', 'inline-code')
    expect(container.querySelector('[data-streamdown="strong"]')).toHaveTextContent('fast')
    expect(container).not.toHaveTextContent('`')
  })

  it('keeps the lines and the characters as typed', async () => {
    const { container } = render(<MessageBody body={'use the <Composer> component\nthen ship it'} mentions={null} names={names} />)
    expect(await screen.findByText(/use the <Composer> component/)).toBeInTheDocument()
    expect(container.querySelector('br')).not.toBeNull()
  })

  it('leaves a name it does not mention as text', async () => {
    render(<MessageBody body="@alice hi" mentions={null} names={names} />)
    expect(await screen.findByText('@alice hi')).toBeInTheDocument()
  })
})
