import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MessageBody, split } from './MessageBody'

const names = new Map([
  ['a1', 'Codex Implementer'],
  ['a2', 'Codex Implementer B'],
  ['u1', 'alice'],
])

describe('split', () => {
  it('turns mentioned names into pills, longest name first', () => {
    const parts = split(
      '@Codex Implementer B 和 @Codex Implementer 看看',
      [
        { kind: 'agent', id: 'a1' },
        { kind: 'agent', id: 'a2' },
      ],
      names,
    )
    expect(parts).toEqual([
      { text: 'Codex Implementer B', pill: true, id: 'a2' },
      { text: ' 和 ', pill: false },
      { text: 'Codex Implementer', pill: true, id: 'a1' },
      { text: ' 看看', pill: false },
    ])
  })

  it('leaves text alone when nothing is mentioned or the name is unknown', () => {
    expect(split('@alice hi', null, names)).toEqual([{ text: '@alice hi', pill: false }])
    expect(split('@ghost hi', [{ kind: 'user', id: 'zzz' }], names)).toEqual([{ text: '@ghost hi', pill: false }])
  })
})

describe('MessageBody', () => {
  it('renders a pill per mention', () => {
    render(<MessageBody body="@alice 够用。" mentions={[{ kind: 'user', id: 'u1' }]} names={names} />)
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.getByText('够用。')).toBeInTheDocument()
  })
})
