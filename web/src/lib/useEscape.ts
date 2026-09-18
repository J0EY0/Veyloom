import { useEffect } from 'react'

// Layers that answer Escape themselves: Radix dialogs, sheets and menus,
// the command palette, and the @ picker. While one is open the page
// leaves the key to it.
export const ownsEscape = '[role="dialog"], [role="menu"], [role="listbox"], [cmdk-root]'

// useEscape calls onEscape when Escape is pressed anywhere while enabled,
// except inside an open overlay, which handles it itself.
export function useEscape(onEscape: () => void, enabled = true) {
  useEffect(() => {
    if (!enabled) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      if (document.querySelector(ownsEscape)) return
      onEscape()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onEscape, enabled])
}
