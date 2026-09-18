import { vi } from 'vitest'

// A WebSocket the test drives by hand: open(), frame(), drop().
export class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static readonly OPEN = 1
  readonly url: string
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  open() {
    this.onopen?.()
  }

  frame(payload: unknown) {
    this.raw(JSON.stringify(payload))
  }

  raw(data: string) {
    this.onmessage?.({ data })
  }

  drop(code = 1006) {
    this.onclose?.({ code })
  }

  close() {
    this.closed = true
  }

  static last(): FakeWebSocket {
    return FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
  }
}

export function stubWebSocket() {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
}
