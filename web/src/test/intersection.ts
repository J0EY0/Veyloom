// A controllable IntersectionObserver for jsdom, which has none. Tests
// call intersectAll() to pretend every observed element scrolled into view,
// leaveAll() out of it.

const instances = new Set<FakeIntersectionObserver>()

class FakeIntersectionObserver implements IntersectionObserver {
  readonly root = null
  readonly rootMargin = ''
  readonly scrollMargin = ''
  readonly thresholds: ReadonlyArray<number> = []
  private targets = new Set<Element>()
  private callback: IntersectionObserverCallback

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback
    instances.add(this)
  }

  observe(target: Element) {
    this.targets.add(target)
  }

  unobserve(target: Element) {
    this.targets.delete(target)
  }

  disconnect() {
    this.targets.clear()
    instances.delete(this)
  }

  takeRecords(): IntersectionObserverEntry[] {
    return []
  }

  fire(isIntersecting = true) {
    const entries = [...this.targets].map((target) => ({ target, isIntersecting }) as IntersectionObserverEntry)
    if (entries.length > 0) {
      this.callback(entries, this)
    }
  }
}

export function installIntersectionObserver() {
  globalThis.IntersectionObserver = FakeIntersectionObserver as unknown as typeof IntersectionObserver
}

export function intersectAll() {
  instances.forEach((observer) => observer.fire())
}

// leaveAll pretends every observed element scrolled far out of view.
export function leaveAll() {
  instances.forEach((observer) => observer.fire(false))
}
