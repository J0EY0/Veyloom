import { useEffect, useState } from 'react'

// A clock that ticks only while something on screen needs it, so idle
// pages schedule nothing. Set going, it tells the time at once: what it
// kept while it stood may be long past, a turn gone quiet ten minutes
// before its footer asked.
export function useNow(enabled: boolean, intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!enabled) return
    const tick = () => setNow(Date.now())
    const first = setTimeout(tick, 0)
    const timer = setInterval(tick, intervalMs)
    return () => {
      clearTimeout(first)
      clearInterval(timer)
    }
  }, [enabled, intervalMs])
  return now
}
