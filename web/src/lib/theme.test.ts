import { beforeEach, describe, expect, it } from 'vitest'
import { applyTheme, getTheme, setTheme } from './theme'

describe('theme', () => {
  beforeEach(() => setTheme('system'))

  it('pins a scheme on the root and remembers it', () => {
    setTheme('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('veyloom.theme')).toBe('dark')
    expect(getTheme()).toBe('dark')

    setTheme('system')
    expect(document.documentElement.dataset.theme).toBeUndefined()
    expect(localStorage.getItem('veyloom.theme')).toBeNull()
  })

  it('applies the stored choice', () => {
    setTheme('light')
    applyTheme()
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
