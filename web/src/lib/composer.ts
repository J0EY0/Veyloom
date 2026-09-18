// A small channel for "put this text in the composer": the take-over
// button on an agent's mention uses it to prefill the reply.

export type ComposerTarget = string // 'room' or a thread id

type Listener = (text: string) => void
const listeners = new Map<ComposerTarget, Listener>()

// Composers register while mounted; the last one for a target wins.
export function registerComposer(target: ComposerTarget, listener: Listener): () => void {
  listeners.set(target, listener)
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
