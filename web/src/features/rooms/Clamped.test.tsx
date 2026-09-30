import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Clamped } from './Clamped'

// jsdom lays nothing out: the reply stands 200 tall at a 16 root, its
// lines where the texts say.
const lines: Record<string, [number, number][]> = {
  first: [[0, 24]],
  second: [[30, 54]],
  third: [[60, 84]],
  fourth: [[90, 114]],
}

beforeEach(() => {
  document.documentElement.style.fontSize = '16px'
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(200)
  Object.defineProperty(Range.prototype, 'getClientRects', {
    configurable: true,
    value(this: Range) {
      return (lines[this.startContainer.textContent ?? ''] ?? []).map(([top, bottom]) => ({ top, bottom, height: bottom - top }))
    },
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  delete (Range.prototype as { getClientRects?: unknown }).getClientRects
  document.documentElement.style.fontSize = ''
})

describe('Clamped', () => {
  it('cuts a long reply after its last whole line within the limit, and opens it all', async () => {
    render(
      <Clamped>
        <p>first</p>
        <p>second</p>
        <p>third</p>
        <p>fourth</p>
      </Clamped>,
    )
    // 4.5rem at 16 is 72: the second line is the last that ends by then.
    const cutOff = screen.getByText('first').closest('div')!.parentElement!
    expect(cutOff.style.maxHeight).toBe(`${54 / 16}rem`)

    await userEvent.click(screen.getByRole('button', { name: '展开全文' }))
    expect(cutOff.style.maxHeight).toBe('')
    expect(screen.getByRole('button', { name: '收起' })).toHaveAttribute('aria-expanded', 'true')
  })
})
