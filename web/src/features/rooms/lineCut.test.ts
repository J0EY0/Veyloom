import { afterEach, describe, expect, it } from 'vitest'
import { lineCut } from './lineCut'

// jsdom lays nothing out: each text says where its lines are, top and
// bottom, and the element starts 100 down the page.
function laidOut(lines: Record<string, [number, number][]>, html: string): HTMLElement {
  Object.defineProperty(Range.prototype, 'getClientRects', {
    configurable: true,
    value(this: Range) {
      const at = lines[this.startContainer.textContent ?? ''] ?? []
      return at.map(([top, bottom]) => ({ top: top + 100, bottom: bottom + 100, height: bottom - top }))
    },
  })
  const element = document.createElement('div')
  element.innerHTML = html
  element.getBoundingClientRect = () => ({ top: 100 }) as DOMRect
  return element
}

afterEach(() => {
  delete (Range.prototype as { getClientRects?: unknown }).getClientRects
})

describe('lineCut', () => {
  it('cuts after the last line that ends by the limit', () => {
    const element = laidOut(
      {
        one: [[0, 20]],
        'two, three': [
          [26, 46],
          [52, 72],
        ],
        four: [[78, 98]],
      },
      '<p>one</p><p>two, three</p><p>four</p>',
    )
    expect(lineCut(element, 81)).toBe(72)
  })

  it('does not cut through a taller piece of the line it would end', () => {
    // The third line holds a mention taller than its words.
    const element = laidOut(
      { one: [[0, 20]], two: [[26, 46]], 'three ': [[52, 72]], '@Coder': [[50, 76]] },
      '<p>one</p><p>two</p><p>three <span>@Coder</span></p>',
    )
    expect(lineCut(element, 74)).toBe(50)
  })

  it('keeps the limit when no line ends by it, or the browser cannot say', () => {
    expect(lineCut(laidOut({ 'one long line': [[0, 100]] }, '<p>one long line</p>'), 81)).toBe(81)
    delete (Range.prototype as { getClientRects?: unknown }).getClientRects
    const element = document.createElement('div')
    element.innerHTML = '<p>one</p>'
    expect(lineCut(element, 81)).toBe(81)
  })
})
