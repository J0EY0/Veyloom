import type { GraphNode } from '@/api/types'

// How the relation graph draws a node (docs/webui.md 4.14): a dot, its
// name beside it, the same size on screen at any zoom. Sizes are in design
// px, px at the interface's default size, where a rem is 18; the canvas
// scales them with the rem, so the interface size setting reaches the
// graph too.

export const designRem = 18

// A module is a dark dot, the middle of its cluster; any other page a grey
// one; a summary of a topic, or a deprecated page, a hollow one; a page of
// another wiki a dashed ring; a directory a small square; a topic a small
// ring.
export type Glyph = 'hub' | 'page' | 'hollow' | 'external' | 'dir' | 'topic'

export function glyphOf(node: GraphNode, hub: boolean): Glyph {
  if (hub) return 'hub'
  if (node.kind === 'external') return 'external'
  if (node.page) return node.page.status === 'deprecated' || node.page.type === 'Topic' ? 'hollow' : 'page'
  return node.kind === 'topic' ? 'topic' : 'dir'
}

// A page's dot grows with how much it relates to, up to a little short of
// a module's.
export function dotRadius(glyph: Glyph, degree: number): number {
  if (glyph === 'hub') return 9
  if (glyph === 'dir') return 3.6
  if (glyph === 'topic') return 4
  return Math.min(7, 3.8 + 0.9 * Math.sqrt(degree))
}

// The names' sizes: a page's, a directory's or topic's, a cluster's.
export const nameSize = 12
export const pathSize = 11
export const regionSize = 19

// A page's name has its type's icon before it, and the gap after.
export const iconLead = 16

// How many characters of a name show: twelve, sixteen near the node in
// focus; the card has every name whole. A directory keeps the end of its
// path, where its own name is. A cluster is named in ten at most.
export const nameCap = 12
export const nearNameCap = 16
export const pathCap = 22
export const regionCap = 10

// textWidth guesses how wide text sets at a size, in design px: a CJK
// character a full em, a capital or # a little over half, a narrow letter
// or mark a third, any other about half; in monospace three fifths each.
export function textWidth(text: string, size: number, mono = false): number {
  let width = 0
  for (const char of text) {
    if (/[⺀-鿿가-힯＀-￯]/.test(char)) width += size
    else if (mono) width += size * 0.6
    else if (/[A-Z#]/.test(char)) width += size * 0.64
    else if (/[il.,:;|!'()[\]/ ]/.test(char)) width += size * 0.32
    else width += size * 0.54
  }
  return width
}

// clip shortens text to so many characters, an ellipsis standing for the
// rest: the end, or for a path the start.
export function clip(text: string, cap: number, keepEnd = false): string {
  const chars = [...text]
  if (chars.length <= cap) return text
  return keepEnd ? `…${chars.slice(chars.length - cap + 1).join('')}` : `${chars.slice(0, cap - 1).join('')}…`
}

// regionName is what a cluster is called over its region: its module's
// title, without the word for module.
export function regionName(title: string): string {
  const short = title.replace(/\s*(模块|module)$/i, '').trim()
  return clip(short || title, regionCap)
}
