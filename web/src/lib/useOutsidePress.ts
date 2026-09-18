import { useCallback, useEffect, useRef } from 'react'
import { ownsEscape } from './useEscape'

export interface OutsidePressOptions {
  // Presses on elements matching this, such as the buttons that open the
  // part, count as neither inside nor outside.
  ignore?: string
  enabled?: boolean
}

// useOutsidePress calls onOutside when a pointer goes down outside a part of
// the page, like a click beside a drawer. The part marks presses inside it
// with the returned handler, set as onPointerDownCapture on its root. That
// goes by React's tree rather than the DOM's, so the menus and dialogs the
// part opens in portals count as inside. A press while some other layer is
// open (a menu, a dialog, the palette) is that layer's to handle.
export function useOutsidePress(onOutside: () => void, { ignore, enabled = true }: OutsidePressOptions = {}) {
  const inside = useRef(false)
  const latest = useRef(onOutside)
  useEffect(() => {
    latest.current = onOutside
  })
  useEffect(() => {
    if (!enabled) return
    // Capture runs before React's handlers, so each press starts outside.
    function reset() {
      inside.current = false
    }
    function check(event: PointerEvent) {
      if (inside.current || document.querySelector(ownsEscape)) return
      if (ignore && event.target instanceof Element && event.target.closest(ignore)) return
      latest.current()
    }
    document.addEventListener('pointerdown', reset, true)
    document.addEventListener('pointerdown', check)
    return () => {
      document.removeEventListener('pointerdown', reset, true)
      document.removeEventListener('pointerdown', check)
    }
  }, [ignore, enabled])
  return useCallback(() => {
    inside.current = true
  }, [])
}
