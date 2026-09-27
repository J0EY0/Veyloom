import { useCallback, useEffect, useRef } from 'react'

// useInView returns a ref for an element and calls onVisible when it comes
// near the view: once, for something that loads when first seen, or every
// time, for a sentinel at the end of a list that loads the next page.
export function useInView<T extends Element>(onVisible: () => void, { once = true, margin = '300px' }: { once?: boolean; margin?: string } = {}) {
  const latest = useRef(onVisible)
  useEffect(() => {
    latest.current = onVisible
  })

  const observer = useRef<IntersectionObserver | null>(null)
  return useCallback(
    (node: T | null) => {
      observer.current?.disconnect()
      observer.current = null
      if (!node) return
      observer.current = new IntersectionObserver(
        (entries) => {
          if (entries.some((entry) => entry.isIntersecting)) {
            if (once) {
              observer.current?.disconnect()
              observer.current = null
            }
            latest.current()
          }
        },
        { rootMargin: margin },
      )
      observer.current.observe(node)
    },
    [once, margin],
  )
}

// useNearView returns a ref for an element and tells onChange whether it is
// near the view each time that changes: for what is drawn while near and
// let go of once far, sparing the memory a long document would fill. root
// is the scroller whose view counts, for an element inside one: it clips
// what it holds, and the viewport would find it out of sight.
export function useNearView<T extends Element>(
  onChange: (near: boolean) => void,
  { root = null, margin = '300px' }: { root?: Element | null; margin?: string } = {},
) {
  const latest = useRef(onChange)
  useEffect(() => {
    latest.current = onChange
  })

  const observer = useRef<IntersectionObserver | null>(null)
  return useCallback(
    (node: T | null) => {
      observer.current?.disconnect()
      observer.current = null
      if (!node) return
      observer.current = new IntersectionObserver(
        (entries) => {
          const last = entries.at(-1)
          if (last) latest.current(last.isIntersecting)
        },
        { root, rootMargin: margin },
      )
      observer.current.observe(node)
    },
    [root, margin],
  )
}
