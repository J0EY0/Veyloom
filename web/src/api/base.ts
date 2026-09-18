// Where the API is. Empty means the page's own origin: the dev server and
// vite preview proxy /api to `veyloom serve`, and so would any static
// host in front of it. VITE_API_BASE names another origin instead, e.g.
// http://127.0.0.1:7788; that server must list the page's host in
// server.allowed_origins.
const configured = (import.meta.env.VITE_API_BASE as string | undefined)?.replace(/\/+$/, '') ?? ''

export const apiBase = `${configured}/api/v1`

// wsBase is the same place for the WebSocket, with the matching scheme.
export function wsBase(): string {
  if (configured !== '') {
    return `${configured.replace(/^http/, 'ws')}/api/v1`
  }
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/api/v1`
}
