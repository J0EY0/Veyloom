import { useStore, type Viewport } from '@xyflow/react'
import { rootRem } from '@/lib/rem'
import { clip, designRem, iconLead, nameCap, nameSize, textWidth } from './glyphs'
import type { Area } from './labels'
import type { Point } from './layout'

// Where the relation graph's view goes (docs/webui.md 4.14): as it opens,
// as a node comes into focus, and back to the whole of it.

// Where the card about the node in focus goes: on the right of the canvas,
// 20rem and its margins, when the canvas is wide enough to leave the graph
// room beside it; under the graph otherwise, on a phone or between two
// sidebars, taking up to 45% of its height. The toolbar takes a row over
// the canvas's top, where a node in focus does not go.
const cardRem = 23
const sideCardFrom = 44
const underCard = 0.55
const toolbarRem = 3.25

export function narrowCanvas(width: number, rem = rootRem()): boolean {
  return width < sideCardFrom * rem
}

// useNarrowCanvas says whether the card goes under the graph.
export function useNarrowCanvas(): boolean {
  const width = useStore((state) => state.width)
  return narrowCanvas(width)
}

// viewArea is the part of the canvas a view has to itself, in its px: all
// of it, or with a card open, what the card leaves.
export function viewArea(width: number, height: number, card: boolean, rem = rootRem()): Area {
  if (!card) return { left: 0, top: 0, right: width, bottom: height }
  return narrowCanvas(width, rem)
    ? { left: 0, top: 0, right: width, bottom: height * underCard }
    : { left: 0, top: 0, right: width - cardRem * rem, bottom: height }
}

// nameArea is where the names may go: the view's area, a little in from
// its edges. The toolbar and the zoom over it are kept clear of apart.
export function nameArea(width: number, height: number, card: boolean, rem = rootRem()): Area {
  const area = viewArea(width, height, card, rem)
  const inset = rem / 2
  return { left: area.left + inset, top: area.top + inset, right: area.right - inset, bottom: area.bottom - inset }
}

// focusArea is the room a node in focus is centred in: what the card
// leaves, below the toolbar.
export function focusArea(width: number, height: number, rem = rootRem()): Area {
  return { ...viewArea(width, height, true, rem), top: toolbarRem * rem }
}

// Bounds is the box round the layout's dots, in the canvas's units.
export interface Bounds {
  left: number
  top: number
  right: number
  bottom: number
}

export function boundsOf(points: Iterable<Point>): Bounds | undefined {
  let bounds: Bounds | undefined
  for (const { x, y } of points) {
    bounds = bounds
      ? { left: Math.min(bounds.left, x), top: Math.min(bounds.top, y), right: Math.max(bounds.right, x), bottom: Math.max(bounds.bottom, y) }
      : { left: x, top: y, right: x, bottom: y }
  }
  return bounds
}

// Fit is what fitting the graph goes by: its bounds, how many nodes it has,
// and half its widest name, in px, for the names at its edges to fit.
export interface Fit {
  bounds: Bounds
  count: number
  half: number
}

// halfName is half the widest name pages have, in px at a rem.
export function halfName(titles: string[], rem: number): number {
  const widest = titles.reduce((most, title) => Math.max(most, textWidth(clip(title, nameCap), nameSize) + iconLead), 0)
  return ((widest / 2) * rem) / designRem
}

// The canvas the graph's spacing is set for, in design px: the graph's own
// at 1440px wide, at the default size. A smaller one, a phone's, keeps that
// spacing and shows part of the graph, rather than squeezing it all in.
const reference = { width: 853, height: 826 }

// How near the graph may open: a small graph spreads out up to 1.8 times
// its layout, a bigger one 1.5; the names keep their size either way.
const smallGraph = 24
const smallCap = 1.8
const cap = 1.5

// Room kept round the graph, in design px: either side, besides half the
// widest name; over it, for the toolbar; under it, for the zoom.
const sideRoom = 30
const topRoom = 76
const bottomRoom = 58

// Focusing comes in to this much over the opening zoom, for the names of
// those near the node to have room.
export const focusNearer = 1.35

function fitZoom(fit: Fit, width: number, height: number, rem: number): number {
  const s = rem / designRem
  const across = width - 2 * (sideRoom * s + fit.half)
  const down = height - (topRoom + bottomRoom) * s
  const zoom = Math.min(
    across / Math.max(1, fit.bounds.right - fit.bounds.left),
    down / Math.max(1, fit.bounds.bottom - fit.bounds.top),
    fit.count < smallGraph ? smallCap : cap,
  )
  return Math.max(0.05, zoom)
}

function centred(fit: Fit, area: Area, zoom: number, rem: number): Viewport {
  const s = rem / designRem
  const middleX = (fit.bounds.left + fit.bounds.right) / 2
  const middleY = (fit.bounds.top + fit.bounds.bottom) / 2
  return {
    x: (area.left + area.right) / 2 - middleX * zoom,
    y: (area.top + topRoom * s + area.bottom - bottomRoom * s) / 2 - middleY * zoom,
    zoom,
  }
}

// Opening is the view the graph opens at, and its zoom, the base the rules
// for names and focusing go by.
export interface Opening {
  view: Viewport
  base: number
}

// openView is how the graph opens: whole, as near as fits the canvas with
// room for the names at its edges. A canvas too small for the whole graph
// at the spacing a desktop's gives it, or at the layout's own where that
// is less (a small graph a desktop spreads out), opens at that spacing on
// the middle cluster, the one most tied to the others, which the layout
// puts at 0,0; the rest is a drag away.
export function openView(fit: Fit, width: number, height: number, rem: number): Opening {
  const actual = fitZoom(fit, width, height, rem)
  const s = rem / designRem
  const spaced = Math.min(1, fitZoom(fit, Math.max(width, reference.width * s), Math.max(height, reference.height * s), rem))
  if (actual >= spaced - 1e-9) return { base: actual, view: centred(fit, { left: 0, top: 0, right: width, bottom: height }, actual, rem) }
  return { base: spaced, view: { x: width / 2, y: height / 2, zoom: spaced } }
}

// wholeView is the whole graph in an area, the fit button's view.
export function wholeView(fit: Fit, area: Area, rem: number): Viewport {
  return centred(fit, area, fitZoom(fit, area.right - area.left, area.bottom - area.top, rem), rem)
}

// focusView is where the view goes as a node comes into focus: the node in
// the middle of the room the card leaves, no further out than the view
// was, and near enough for the names round it, focusNearer times the
// opening zoom.
export function focusView(point: Point, zoom: number, base: number, area: Area, maxZoom: number): Viewport {
  const next = Math.min(maxZoom, Math.max(zoom, base * focusNearer))
  return { x: (area.left + area.right) / 2 - point.x * next, y: (area.top + area.bottom) / 2 - point.y * next, zoom: next }
}
