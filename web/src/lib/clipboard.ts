// copyText puts text on the clipboard and says whether it worked. The
// Clipboard API exists only on secure origins and can still be refused (an
// unfocused frame, a denied permission) — a self-hosted hub opened by its LAN
// address has no API at all. The older select-and-copy command works in
// those places, provided it runs within the click that asked for it.
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return copyBySelection(text)
  }
}

function copyBySelection(text: string): boolean {
  const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '0'
  area.style.opacity = '0'
  document.body.append(area)
  // The copy command copies the selection, which lives in the focused field.
  area.focus({ preventScroll: true })
  area.select()
  try {
    return typeof document.execCommand === 'function' && document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
    focused?.focus()
  }
}
