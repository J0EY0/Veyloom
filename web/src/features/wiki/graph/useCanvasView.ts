import { useEffect, useState, type RefObject } from 'react'
import { useStoreApi, type Viewport } from '@xyflow/react'
import type { Area } from './labels'

// How the relation graph's canvas follows its view and what is over it
// (docs/webui.md 4.14).

// The names are placed anew once the view has stopped moving this long.
const settle = 120

// useSettledView keeps the dots and names their size on screen: the zoom
// is scaled back out of them through --graph-k on the frame round the
// canvas, set as the view moves without drawing anything anew. It gives
// the view as it last stopped, for the names to be placed for, and a way
// to say a view set at once has stopped already.
export function useSettledView(frame: RefObject<HTMLDivElement | null>): [Viewport, (view: Viewport) => void] {
  const store = useStoreApi()
  const [view, setView] = useState<Viewport>(() => {
    const [x, y, zoom] = store.getState().transform
    return { x, y, zoom }
  })
  useEffect(() => {
    const scaleBack = (zoom: number) => frame.current?.style.setProperty('--graph-k', String(1 / zoom))
    scaleBack(store.getState().transform[2])
    let timer: ReturnType<typeof setTimeout> | undefined
    const unsubscribe = store.subscribe((state, prev) => {
      if (state.transform === prev.transform) return
      const [x, y, zoom] = state.transform
      scaleBack(zoom)
      clearTimeout(timer)
      timer = setTimeout(() => setView({ x, y, zoom }), settle)
    })
    return () => {
      unsubscribe()
      clearTimeout(timer)
    }
  }, [store, frame])
  return [view, setView]
}

// useBlocked is what is over the canvas, for names to keep clear of: the
// toolbar, and the zoom, which moves out of the card's way (again, as
// moved says).
export function useBlocked(frame: RefObject<HTMLDivElement | null>, toolbar: RefObject<HTMLDivElement | null>, moved: string): Area[] {
  const [blocked, setBlocked] = useState<Area[]>([])
  useEffect(() => {
    const canvas = frame.current
    const bar = toolbar.current
    if (!canvas || !bar || typeof ResizeObserver === 'undefined') return
    const measure = () => {
      const origin = canvas.getBoundingClientRect()
      const boxes = [bar, canvas.querySelector('.react-flow__controls')].flatMap((element) => {
        if (!element) return []
        const box = element.getBoundingClientRect()
        return [{ left: box.left - origin.left, top: box.top - origin.top, right: box.right - origin.left, bottom: box.bottom - origin.top }]
      })
      setBlocked((prev) => (JSON.stringify(prev) === JSON.stringify(boxes) ? prev : boxes))
    }
    const observer = new ResizeObserver(measure)
    observer.observe(canvas)
    observer.observe(bar)
    measure()
    return () => observer.disconnect()
  }, [frame, toolbar, moved])
  return blocked
}
