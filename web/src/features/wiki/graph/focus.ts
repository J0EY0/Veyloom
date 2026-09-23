import { createContext } from 'react'

// What the relation graph is focused on (docs/design.md 5.17): the node a
// person picked and those near it, which stay lit while the rest fade.
// Nodes read it from here rather than from their own data, so that
// focusing leaves the nodes, their measured sizes and where they were
// dragged, as they are.
export interface GraphFocus {
  focus: string
  near: ReadonlySet<string>
}

export const GraphFocusContext = createContext<GraphFocus>({ focus: '', near: new Set() })

// fades says whether a node fades: something else is in focus, and the
// node is not near it.
export function fades({ focus, near }: GraphFocus, id: string): boolean {
  return focus !== '' && !near.has(id)
}
