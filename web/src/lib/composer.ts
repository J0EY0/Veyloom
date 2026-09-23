// A small channel for "put this text in the composer": the take-over
// button on an agent's mention uses it to prefill the reply, and a wiki
// page's question button the reply in the wiki topic, which it opens.

export type ComposerTarget = string // 'room' or a thread id

type Listener = (text: string) => void
const listeners = new Map<ComposerTarget, Listener>()
// Text for a composer that is not open yet, handed over when it opens.
const waiting = new Map<ComposerTarget, string>()

// Composers register while mounted; the last one for a target wins, and
// is handed what waits for it.
export function registerComposer(target: ComposerTarget, listener: Listener): () => void {
  listeners.set(target, listener)
  const text = waiting.get(target)
  if (text !== undefined) {
    waiting.delete(target)
    listener(text)
  }
  return () => {
    if (listeners.get(target) === listener) listeners.delete(target)
  }
}

// insertIntoComposer hands text to the composer for target, or to the
// room's when that one is not open; returns whether anyone took it.
export function insertIntoComposer(target: ComposerTarget, text: string): boolean {
  const listener = listeners.get(target) ?? listeners.get('room')
  if (!listener) return false
  listener(text)
  return true
}

// queueForComposer hands text to the composer for target now if it is
// open, else as soon as it opens.
export function queueForComposer(target: ComposerTarget, text: string) {
  const listener = listeners.get(target)
  if (listener) listener(text)
  else waiting.set(target, text)
}
