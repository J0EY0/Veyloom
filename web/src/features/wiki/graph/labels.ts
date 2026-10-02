import { clip, iconLead, nameCap, nameSize, nearNameCap, pathCap, pathSize, regionSize, textWidth, type Glyph } from './glyphs'

// Which names the relation graph writes, and where (docs/webui.md 4.14).
// The names keep their size on screen at any zoom, so zooming out leaves
// less room between dots for them: a name goes below its dot, or above,
// right or left, wherever it covers no other dot or name and stays on the
// canvas, and where none is free it is left out, to show on a hover or
// closer in. The clusters' names go first, beside their modules.

export type Side = 'below' | 'above' | 'right' | 'left'

// A node as the names see it: where its centre is on screen, how big its
// dot is there, in px, and what it is called.
export interface Mark {
  id: string
  x: number
  y: number
  r: number
  glyph: Glyph
  degree: number
  name: string
}

// A cluster as the names see it: what it is called, its module's node
// ('' for the general cluster) and its nodes.
export interface Region {
  id: string
  name: string
  hub: string
  members: string[]
}

export interface NameLabel {
  side: Side
  text: string
}

// A cluster's name, carried by one of its nodes, its module or else its
// busiest: where its middle goes from the node's centre, in design px.
export interface RegionLabel {
  name: string
  dx: number
  dy: number
  // Something else is lit, nowhere in this cluster.
  dim: boolean
}

export interface Labels {
  // By node.
  names: Map<string, NameLabel>
  // By the node carrying it.
  regions: Map<string, RegionLabel>
}

export interface Area {
  left: number
  top: number
  right: number
  bottom: number
}

export interface LabelOptions {
  // Where names may go on screen; anywhere while the canvas is unmeasured.
  area?: Area
  // What is over the canvas there, the toolbar and the zoom.
  blocked?: Area[]
  // The node lit, in focus or hovered, and those near it, the node itself
  // among them.
  lead: string
  near: { has: (id: string) => boolean }
  // A node hovered while another is in focus: its name shows too.
  hover: string
  // The zoom over the one the graph opened at.
  zoom: number
  // A big graph writes its clusters' names alone until zoomed in.
  big: boolean
  // Design px to screen px: a rem over the default one.
  scale: number
}

// A graph is big from so many nodes.
export const bigGraph = 60

// Zoomed in this far over the opening, a directory's name shows, and a big
// graph's pages' names.
const pathsFrom = 1.6
const bigFrom = 1.5

// Design px: the gap from a dot to its name below or above it and beside
// it, a name's line and the room kept either side of it; a cluster name's
// line and room; and how far off screen a dot may be for its name to count.
export const nameGap = 5
export const sideGap = 6
export const nameLine = 15
const namePad = 3
export const regionLine = 26
const regionPad = 6
const offScreen = 60

interface Rect {
  l: number
  r: number
  t: number
  b: number
}

// isPath says whether a glyph is a directory's: its name is a path, in
// monospace, without an icon, and waits for a closer view. A page's or a
// topic's name has its icon before it.
export function isPath(glyph: Glyph): boolean {
  return glyph === 'dir'
}

export function placeLabels(marks: Mark[], regions: Region[], options: LabelOptions): Labels {
  const { area, lead, near, hover, scale: s } = options
  const taken: Rect[] = []
  const hits = (rect: Rect) => taken.some((other) => rect.l < other.r && rect.r > other.l && rect.t < other.b && rect.b > other.t)
  const fits = (rect: Rect) => !area || (rect.l >= area.left && rect.r <= area.right && rect.t >= area.top && rect.b <= area.bottom)
  const free = (rect: Rect) => fits(rect) && !hits(rect)
  const byId = new Map(marks.map((mark) => [mark.id, mark]))
  const close = (id: string) => (lead !== '' && near.has(id)) || id === hover
  for (const { left, top, right, bottom } of options.blocked ?? []) taken.push({ l: left, r: right, t: top, b: bottom })
  for (const mark of marks) {
    const room = mark.r + 3 * s
    taken.push({ l: mark.x - room, r: mark.x + room, t: mark.y - room, b: mark.y + room })
  }

  const regionLabels = new Map<string, RegionLabel>()
  for (const region of regions) {
    const members = region.members.flatMap((id) => byId.get(id) ?? [])
    if (members.length === 0) continue
    const hub = byId.get(region.hub)
    const carrier = hub ?? members.reduce((best, mark) => (mark.degree > best.degree ? mark : best))
    const cx = members.reduce((sum, mark) => sum + mark.x, 0) / members.length
    const cy = members.reduce((sum, mark) => sum + mark.y, 0) / members.length
    const anchor = hub ?? { x: cx, y: cy, r: 0 }
    const half = (textWidth(region.name, regionSize) * 1.08 * s) / 2
    const top = Math.min(...members.map((mark) => mark.y))
    const bottom = Math.max(...members.map((mark) => mark.y))
    // Over the module, to its left, its right, under it; over or under the
    // whole cluster.
    const spots = [
      { x: anchor.x, y: anchor.y - anchor.r - 22 * s },
      { x: anchor.x - anchor.r - 12 * s - half, y: anchor.y - s },
      { x: anchor.x + anchor.r + 12 * s + half, y: anchor.y - s },
      { x: anchor.x, y: anchor.y + anchor.r + 22 * s },
      { x: cx, y: top - 42 * s },
      { x: cx, y: bottom + 42 * s },
    ]
    const box = (spot: { x: number; y: number }): Rect => ({
      l: spot.x - half - regionPad * s,
      r: spot.x + half + regionPad * s,
      t: spot.y - (regionLine / 2) * s,
      b: spot.y + (regionLine / 2) * s,
    })
    // A module lit has its name even where it covers something.
    const spot = spots.find((candidate) => free(box(candidate))) ?? (hub && close(hub.id) ? spots[0] : undefined)
    if (!spot) continue
    taken.push(box(spot))
    regionLabels.set(carrier.id, {
      name: region.name,
      dx: (spot.x - carrier.x) / s,
      dy: (spot.y - carrier.y) / s,
      dim: lead !== '' && !region.members.some((id) => near.has(id)),
    })
  }

  const names = new Map<string, NameLabel>()
  const shown = (mark: Mark) =>
    !area || (mark.x > area.left - offScreen && mark.x < area.right + offScreen && mark.y > area.top - offScreen && mark.y < area.bottom + offScreen)
  // Those lit first, pages before paths, the busiest first.
  const order = marks
    .filter((mark) => mark.glyph !== 'hub' && shown(mark))
    .sort((a, b) => Number(close(b.id)) - Number(close(a.id)) || Number(isPath(a.glyph)) - Number(isPath(b.glyph)) || b.degree - a.degree)
  for (const mark of order) {
    const lit = close(mark.id)
    const path = isPath(mark.glyph)
    if (lead !== '' && !lit) continue
    if (!lit && path && options.zoom < pathsFrom) continue
    if (!lit && options.big && options.zoom < bigFrom) continue
    const text = path ? clip(mark.name, pathCap, true) : clip(mark.name, lit ? nearNameCap : nameCap)
    const width = (path ? textWidth(text, pathSize, true) : textWidth(text, nameSize) + iconLead) * s
    const below = mark.y + mark.r + nameGap * s
    const above = mark.y - mark.r - nameGap * s
    const right = mark.x + mark.r + sideGap * s
    const left = mark.x - mark.r - sideGap * s
    const line = nameLine * s
    const pad = namePad * s
    const sides: [Side, Rect][] = [
      ['below', { l: mark.x - width / 2 - pad, r: mark.x + width / 2 + pad, t: below, b: below + line }],
      ['above', { l: mark.x - width / 2 - pad, r: mark.x + width / 2 + pad, t: above - line, b: above }],
      ['right', { l: right - pad, r: right + width + pad, t: mark.y - line / 2, b: mark.y + line / 2 }],
      ['left', { l: left - width - pad, r: left + pad, t: mark.y - line / 2, b: mark.y + line / 2 }],
    ]
    // A page lit has its name even where it covers something, as long as
    // it is on the canvas.
    const side = sides.find(([, rect]) => free(rect)) ?? (lit && !path ? sides.find(([, rect]) => fits(rect)) : undefined)
    if (!side) continue
    taken.push(side[1])
    names.set(mark.id, { side: side[0], text })
  }
  return { names, regions: regionLabels }
}
