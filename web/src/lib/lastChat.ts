// The chat opened last, so opening Veyloom lands back in it. Kept per
// browser; the reader checks that its project still exists.
const STORAGE_KEY = 'veyloom.lastChat'

export function rememberChat(roomId: string) {
  try {
    localStorage.setItem(STORAGE_KEY, roomId)
  } catch {
    // Without storage, opening Veyloom lands on the inbox every time.
  }
}

export function lastChat(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}
