import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, beforeEach, vi } from 'vitest'
import { clearDrafts } from '@/lib/drafts'
import { setLocale } from '@/lib/i18n'
import { installIntersectionObserver } from './intersection'
import { installObjectUrls } from './objectUrls'

// Tests read the UI in Chinese, whatever language jsdom reports.
beforeEach(() => setLocale('zh-CN'))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  localStorage.clear()
  clearDrafts()
})

installIntersectionObserver()

// jsdom lacks the layout APIs the component libraries lean on: Radix and
// use-stick-to-bottom observe sizes, cmdk scrolls the selected item into
// view, the sidebar and the theme ask the media queries.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
}
if (typeof Element.prototype.scrollIntoView !== 'function') {
  Element.prototype.scrollIntoView = () => {}
}
if (typeof Element.prototype.hasPointerCapture !== 'function') {
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.setPointerCapture = () => {}
  Element.prototype.releasePointerCapture = () => {}
}
// Picked files become object URLs the prompt input previews and fetches.
installObjectUrls()
if (typeof window.matchMedia !== 'function') {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
}
