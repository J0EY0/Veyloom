import { createContext } from 'react'
import type { Labels } from './labels'

// What the relation graph lights and writes (docs/design.md 5.17, webui.md
// 4.14): the node in focus, the one lit, which is the one hovered while
// nothing is in focus, those near it, which stay lit while the rest fade,
// and the names that have room. Nodes read it from here rather than from
// their own data, so that lighting and naming leave the nodes, where they
// were laid out or dragged, as they are.
export interface GraphView {
  focus: string
  lead: string
  near: { has: (id: string) => boolean }
  // A node hovered while another is in focus.
  hover: string
  labels: Labels
  // The view has been set: the clusters fade in.
  shown: boolean
}

export const GraphViewContext = createContext<GraphView>({
  focus: '',
  lead: '',
  near: new Set(),
  hover: '',
  labels: { names: new Map(), regions: new Map() },
  shown: false,
})

// fades says whether a node fades: something else is lit, and the node is
// not near it.
export function fades({ lead, near, hover }: GraphView, id: string): boolean {
  return lead !== '' && !near.has(id) && id !== hover
}
