import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText } from './clipboard'

describe('copyText', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    Reflect.deleteProperty(navigator, 'clipboard')
    Reflect.deleteProperty(document, 'execCommand')
  })

  it('uses the Clipboard API where there is one', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    expect(await copyText('claude auth login')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('claude auth login')
  })

  it('falls back to selecting the text and copying it when the API is refused', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) }, configurable: true })
    let copied = ''
    const execCommand = vi.fn(() => {
      copied = (document.activeElement as HTMLTextAreaElement).value
      return true
    })
    Object.defineProperty(document, 'execCommand', { value: execCommand, configurable: true })
    const button = document.createElement('button')
    document.body.append(button)
    button.focus()

    expect(await copyText('codex login')).toBe(true)
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(copied).toBe('codex login')
    // Nothing is left behind, and focus goes back where it was.
    expect(document.querySelector('textarea')).toBeNull()
    expect(document.activeElement).toBe(button)
    button.remove()
  })

  it('says so when neither way works', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    expect(await copyText('pi')).toBe(false)
  })
})
