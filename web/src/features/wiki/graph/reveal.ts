import { useCallback } from 'react'
import { useReactFlow, useStore, useStoreApi, type FitViewOptions } from '@xyflow/react'
import { rootRem } from '@/lib/rem'

// Where the card about the node in focus goes (docs/webui.md 4.14): on the
// right of the canvas, 20rem and its margins, when the canvas is wide
// enough to leave the graph room beside it; under the graph otherwise, on
// a phone or between two sidebars, taking up to half its height.
const cardRem = 23
const sideCardFrom = 44
const underCard = 0.5

function narrowCanvas(width: number): boolean {
  return width < sideCardFrom * rootRem()
}

// useNarrowCanvas says whether the card goes under the graph.
export function useNarrowCanvas(): boolean {
  const width = useStore((state) => state.width)
  return narrowCanvas(width)
}

// focusPadding keeps what a view fits out from under the card.
function focusPadding(narrow: boolean): FitViewOptions['padding'] {
  return narrow ? { top: '6%', x: '6%', bottom: `${underCard * 100 + 2}%` } : { top: '8%', bottom: '8%', left: '6%', right: `${cardRem * rootRem()}px` }
}

// fitOptions is a view taking in the nodes given, clear of the card, or
// with none given the whole graph.
export function fitOptions(ids: string[] | undefined, narrow: boolean): FitViewOptions {
  return ids ? { nodes: ids.map((id) => ({ id })), padding: focusPadding(narrow), maxZoom: 1 } : { padding: 0.15, maxZoom: 1 }
}

// useFocusView moves the view. fit takes in what fitOptions says, once the
// nodes are measured. reveal brings the nodes about a focus into view:
// when any is out of sight or under the card, the view moves to take them
// all in, zooming out if it has to; finding a node by name also zooms in,
// to where it reads. Both read the canvas as it is when they are called.
export function useFocusView() {
  const flow = useReactFlow()
  const store = useStoreApi()
  const fit = useCallback(
    (ids: string[] | undefined, duration: number) => {
      void flow.fitView({ ...fitOptions(ids, narrowCanvas(store.getState().width)), duration })
    },
    [flow, store],
  )
  const reveal = useCallback(
    (ids: string[], zoomIn: boolean) => {
      const {
        width,
        height,
        transform: [x, y, zoom],
      } = store.getState()
      const narrow = narrowCanvas(width)
      const right = narrow ? width : width - cardRem * rootRem()
      const bottom = narrow ? height * underCard : height
      const seen = ids.every((id) => {
        const node = flow.getInternalNode(id)
        if (!node) return true
        const left = node.internals.positionAbsolute.x * zoom + x
        const top = node.internals.positionAbsolute.y * zoom + y
        return left >= 0 && top >= 0 && left + (node.measured.width ?? 0) * zoom <= right && top + (node.measured.height ?? 0) * zoom <= bottom
      })
      if (seen && !zoomIn) return
      void flow.fitView({ nodes: ids.map((id) => ({ id })), padding: focusPadding(narrow), maxZoom: zoomIn ? 1 : zoom, duration: 300 })
    },
    [flow, store],
  )
  return { fit, reveal }
}
