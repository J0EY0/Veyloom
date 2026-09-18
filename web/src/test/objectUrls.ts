// Object URLs for tests. Node has a createObjectURL of its own, but the
// blob: URLs it mints are only readable by its own fetch, which stubApi
// replaces. This one remembers the bytes so the stub can answer for them.

const store = new Map<string, Promise<{ bytes: ArrayBuffer; type: string }>>()
let counter = 0

export function installObjectUrls() {
  URL.createObjectURL = (blob: Blob) => {
    const url = `blob:test/${++counter}`
    store.set(
      url,
      blob.arrayBuffer().then((bytes) => ({ bytes, type: blob.type })),
    )
    return url
  }
  URL.revokeObjectURL = (url: string) => {
    store.delete(url)
  }
}

// objectUrlResponse serves a URL minted above, or nothing for any other.
export async function objectUrlResponse(url: string): Promise<Response | undefined> {
  const entry = store.get(url)
  if (!entry) return undefined
  const { bytes, type } = await entry
  return new Response(bytes, { headers: { 'content-type': type } })
}
