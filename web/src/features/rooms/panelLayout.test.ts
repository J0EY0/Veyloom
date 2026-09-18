import { describe, expect, it } from 'vitest'
import { NARROW_PANEL_REM, PANEL_REM, panelLayout } from './panelLayout'

describe('panelLayout', () => {
  it('lies over the chat when pushing would leave less than 36rem of it', () => {
    // 64rem wide, a 25.5rem topic plus its margin leaves 37.75rem: push.
    expect(panelLayout(64, PANEL_REM).mode).toBe('push')
    expect(panelLayout(62, PANEL_REM)).toEqual({ mode: 'overlay', padRem: 0 })
    expect(panelLayout(55, NARROW_PANEL_REM).mode).toBe('overlay')
    // Not measured yet counts as narrow.
    expect(panelLayout(0, NARROW_PANEL_REM).mode).toBe('overlay')
  })

  it('moves the chat only as far as it must', () => {
    // Wide: the centred 53.75rem column clears the members panel as it is.
    expect(panelLayout(100, NARROW_PANEL_REM)).toEqual({ mode: 'push', padRem: 0 })
    // A bit narrower: just enough room for the column to clear the panel.
    expect(panelLayout(90, NARROW_PANEL_REM)).toEqual({ mode: 'push', padRem: 2.75 })
    // Narrower still: never more room than the panel takes.
    expect(panelLayout(70, PANEL_REM)).toEqual({ mode: 'push', padRem: 26.25 })
  })
})
