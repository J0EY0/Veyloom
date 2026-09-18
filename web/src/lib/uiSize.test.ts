import { beforeEach, describe, expect, it } from 'vitest'
import { applyUiSize, getUiSize, setUiSize } from './uiSize'

describe('uiSize', () => {
  beforeEach(() => setUiSize('default'))

  it('moves the root font size and remembers the choice', () => {
    setUiSize('large')
    expect(document.documentElement.style.fontSize).toBe('125%')
    expect(localStorage.getItem('veyloom.uiSize')).toBe('large')
    expect(getUiSize()).toBe('large')

    // Small is the compact size the interface was drawn at.
    setUiSize('small')
    expect(document.documentElement.style.fontSize).toBe('100%')

    // The default is an eighth larger than that, and stores nothing.
    setUiSize('default')
    expect(document.documentElement.style.fontSize).toBe('112.5%')
    expect(localStorage.getItem('veyloom.uiSize')).toBeNull()
  })

  it('applies the stored choice', () => {
    setUiSize('large')
    document.documentElement.style.fontSize = ''
    applyUiSize()
    expect(document.documentElement.style.fontSize).toBe('125%')
  })
})
