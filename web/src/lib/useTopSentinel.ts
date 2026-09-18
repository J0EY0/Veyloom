import { useCallback, useEffect, useRef } from 'react'

// useTopSentinel returns a ref for an element at the top of a scroller
// and calls onVisible whenever it scrolls into view (a little before, so
// the next page is already loading when the reader reaches the top).
export function useTopSentinel(onVisible: () => void) {
  const latest = useRef(onVisible)
  useEffect(() => {
    latest.current = onVisible
  })

  const observer = useRef<IntersectionObserver | null>(null)
  return useCallback((node: HTMLElement | null) => {
    observer.current?.disconnect()
    observer.current = null
    if (!node) return
    observer.current = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          latest.current()
        }
      },
      { rootMargin: '200px 0px 0px 0px' },
    )
    observer.current.observe(node)
  }, [])
}
