import { describe, expect, it } from 'vitest'
import { focusView, openView, viewArea, wholeView, type Fit } from './reveal'

// Where the relation graph's view goes (docs/webui.md 4.14), at the
// default size, a rem of 18: room is kept either side for 30px and half
// the widest name, 76px over the graph for the toolbar and 58px under it
// for the zoom.

const fit = (bounds: Fit['bounds'], count: number, half: number): Fit => ({ bounds, count, half })

describe('the relation graph view', () => {
  it('opens whole, as near as fits with room for the names, no nearer than 1.5', () => {
    const opening = openView(fit({ left: -100, top: -100, right: 100, bottom: 100 }, 30, 50), 1000, 900, 18)
    expect(opening).toEqual({ base: 1.5, view: { x: 500, y: 459, zoom: 1.5 } })
    // Down is what limits a tall graph: 900 less 134, over 600.
    expect(openView(fit({ left: -100, top: -300, right: 100, bottom: 300 }, 30, 50), 1000, 900, 18).base).toBeCloseTo(766 / 600, 6)
  })

  it('spreads a small graph out further, and opens it whole where it fits at the spacing of its layout', () => {
    expect(openView(fit({ left: -100, top: -100, right: 100, bottom: 100 }, 10, 50), 1000, 900, 18).base).toBe(1.8)
    // A narrow canvas takes it whole, if not spread as far.
    const narrow = openView(fit({ left: -40, top: -350, right: 140, bottom: 30 }, 7, 80), 491, 694, 18)
    expect(narrow.base).toBeCloseTo(560 / 380, 6)
    expect(narrow.view.x).toBeCloseTo(245.5 - 50 * narrow.base, 6)
  })

  it('opens a phone on the middle cluster at the spacing a desktop gives it', () => {
    const opening = openView(fit({ left: -400, top: -300, right: 400, bottom: 300 }, 40, 60), 390, 700, 18)
    // Across 853, the canvas the spacing is set for: 673 over 800.
    expect(opening.base).toBeCloseTo(673 / 800, 6)
    expect(opening.view).toEqual({ x: 195, y: 350, zoom: opening.base })
  })

  it('takes in the whole graph clear of the card', () => {
    const view = wholeView(fit({ left: 0, top: 0, right: 400, bottom: 300 }, 30, 50), viewArea(1200, 800, true, 18), 18)
    // The card takes 23rem off the right: 786 across, 626 of it for the graph.
    expect(view.zoom).toBeCloseTo(1.5, 6)
    expect(view.x).toBeCloseTo(393 - 200 * 1.5, 6)
  })

  it('brings a node into focus to the middle of the room the card leaves, never further out', () => {
    const area = { left: 0, top: 0, right: 600, bottom: 800 }
    expect(focusView({ x: 100, y: 50 }, 1, 1, area, 4)).toEqual({ x: 300 - 135, y: 400 - 67.5, zoom: 1.35 })
    expect(focusView({ x: 100, y: 50 }, 2, 1, area, 4).zoom).toBe(2)
    expect(focusView({ x: 100, y: 50 }, 1, 4, area, 4).zoom).toBe(4)
  })
})
