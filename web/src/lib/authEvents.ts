// A one-way signal from the API client to the auth gate: some request got
// a 401, so the session is over and the login page should show.

const listeners = new Set<() => void>()

export function onSignedOut(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function signedOut() {
  listeners.forEach((listener) => listener())
}
