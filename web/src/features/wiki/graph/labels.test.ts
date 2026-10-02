import { describe, expect, it } from 'vitest'
import { placeLabels, type LabelOptions, type Mark, type Region } from './labels'

// Which names the relation graph writes, and where (docs/webui.md 4.14),
// at a rem of 16, on screen px.

const mark = (id: string, x: number, y: number, overrides: Partial<Mark> = {}): Mark => ({ id, x, y, r: 4, glyph: 'page', degree: 1, name: id, ...overrides })

const options = (overrides: Partial<LabelOptions> = {}): LabelOptions => ({
  area: { left: 0, top: 0, right: 1000, bottom: 1000 },
  lead: '',
  near: new Set<string>(),
  hover: '',
  zoom: 1,
  big: false,
  scale: 1,
  ...overrides,
})

const sides = (marks: Mark[], regions: Region[] = [], extra: Partial<LabelOptions> = {}) =>
  Object.fromEntries([...placeLabels(marks, regions, options(extra)).names].map(([id, label]) => [id, label.side]))

describe('the relation graph names', () => {
  it('writes a name below its dot, or above, right or left where that is taken', () => {
    expect(sides([mark('a', 500, 500)])).toEqual({ a: 'below' })
    // A dot just under it: the name goes over.
    expect(sides([mark('a', 500, 500, { degree: 9 }), mark('b', 500, 520)])).toMatchObject({ a: 'above' })
    // Dots under and over it: beside it.
    expect(sides([mark('a', 500, 500, { degree: 9 }), mark('b', 500, 520), mark('c', 500, 478)])).toMatchObject({ a: 'right' })
  })

  it('keeps names on the canvas, and leaves out one with no room', () => {
    expect(sides([mark('a', 500, 995)])).toEqual({ a: 'above' })
    const crowd = [mark('a', 500, 500, { degree: 9 }), mark('b', 500, 520), mark('c', 500, 478), mark('d', 524, 500), mark('e', 476, 500)]
    expect(sides(crowd)).not.toHaveProperty('a')
  })

  it('shortens a name to twelve characters, sixteen near the node in focus', () => {
    const long = mark('a', 500, 500, { name: '旧数据里的大写标签不会自动迁移成小写' })
    expect(placeLabels([long], [], options()).names.get('a')?.text).toBe('旧数据里的大写标签不会…')
    expect(placeLabels([long], [], options({ lead: 'a', near: new Set(['a']) })).names.get('a')?.text).toBe('旧数据里的大写标签不会自动迁移…')
  })

  it('names only what is near the node lit, a page lit even where it covers something', () => {
    const marks = [
      mark('a', 500, 500, { degree: 9 }),
      mark('b', 500, 520),
      mark('c', 500, 478),
      mark('d', 524, 500),
      mark('e', 476, 500),
      mark('far', 100, 100),
    ]
    const lit = sides(marks, [], { lead: 'a', near: new Set(['a', 'b']) })
    expect(lit).toMatchObject({ a: 'below', b: 'below' })
    expect(lit).not.toHaveProperty('far')
  })

  it('writes a directory only closer in, and a big graph only its clusters, until zoomed in', () => {
    const dir = mark('internal/store/', 500, 500, { glyph: 'dir' })
    expect(sides([dir])).toEqual({})
    expect(sides([dir], [], { zoom: 1.6 })).toEqual({ 'internal/store/': 'below' })
    expect(sides([mark('a', 500, 500)], [], { big: true })).toEqual({})
    expect(sides([mark('a', 500, 500)], [], { big: true, zoom: 1.5 })).toEqual({ a: 'below' })
  })

  it('names each cluster first, over its module, the module itself unnamed', () => {
    const marks = [mark('hub', 500, 500, { glyph: 'hub', r: 9, degree: 4 }), mark('a', 560, 500)]
    const labels = placeLabels(marks, [{ id: 'hub', name: '存储', hub: 'hub', members: ['hub', 'a'] }], options())
    expect(labels.regions.get('hub')).toEqual({ name: '存储', dx: 0, dy: -31, dim: false })
    expect(labels.names.has('hub')).toBe(false)
    // Something else lit, the cluster's name fades.
    const other = placeLabels(marks, [{ id: 'hub', name: '存储', hub: 'hub', members: ['hub', 'a'] }], options({ lead: 'x', near: new Set(['x']) }))
    expect(other.regions.get('hub')?.dim).toBe(true)
  })

  it('names the general cluster in its middle, carried by its busiest page', () => {
    const marks = [mark('a', 400, 500), mark('b', 600, 500, { degree: 3 })]
    const labels = placeLabels(marks, [{ id: '', name: '通用', hub: '', members: ['a', 'b'] }], options())
    expect(labels.regions.get('b')).toEqual({ name: '通用', dx: -100, dy: -22, dim: false })
  })
})
