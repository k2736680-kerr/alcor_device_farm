import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './server'

let interceptedFetch: typeof fetch

beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' })
  interceptedFetch = globalThis.fetch
  globalThis.fetch = (input, init) => {
    // Node 24's fetch rejects jsdom's AbortSignal before MSW can intercept it.
    // Cancellation behavior remains covered by the browser E2E suite.
    return interceptedFetch(input, init ? { ...init, signal: undefined } : init)
  }
})
afterEach(() => {
  server.resetHandlers()
  cleanup()
})
afterAll(() => {
  globalThis.fetch = interceptedFetch
  server.close()
})

// jsdom does not implement matchMedia; antd's responsive observer needs it.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

// jsdom does not implement ResizeObserver; antd components rely on it.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(window, 'ResizeObserver', { writable: true, value: ResizeObserverStub })

// Ant Design asks for pseudo-element styles while measuring table scrollbars;
// jsdom does not implement that overload.
const getComputedStyle = window.getComputedStyle.bind(window)
Object.defineProperty(window, 'getComputedStyle', {
  writable: true,
  value: (element: Element) => getComputedStyle(element),
})
