import { useLayoutEffect, useState } from 'react'

// How a side panel sits beside the chat (docs/webui.md §0), in rem so the
// interface size setting scales it with everything else.
// The chat column's widest box: the message rows' max-w-215.
export const CHAT_COLUMN_REM = 53.75
// Less chat than this beside the panel and the panel covers the chat instead.
export const MIN_CHAT_REM = 36
// The panel's right-3 margin.
export const PANEL_MARGIN_REM = 0.75
// SidePanel's widths: w-102, and w-75 for a list of names (narrow).
export const PANEL_REM = 25.5
export const NARROW_PANEL_REM = 18.75
// What the chat's top bar holds at a width, measured in English, whose
// views and island words run longest (docs/webui.md 4.4). The bar gives up
// first the island's idle count, then a row of its own for what the island
// says, then the row of views, which folds into a menu, and on a phone the
// faces and the members button.
//
// How wide a bar must be to hold the views beside a title of 8rem, the
// island's faces and the bar's buttons; narrower, the views fold into a
// menu, and with the island in the bar the members button gives way to it
// (the island opens the members too).
export const VIEWS_ROW_REM = 41
// How wide a bar must be to say, after the faces of an idle island, how
// many are idle.
export const ISLAND_COUNT_REM = 49
// How wide a bar must be for an idle island's faces at all, beside the
// folded views and a title of 6rem; narrower, a phone's, the bar holds the
// title, the views, search and the chat's menu alone, which has the members.
export const FACES_REM = 25
// How wide a bar must be for the island in it to give a button its full
// name ("Cancel and start a new session"); narrower, or under the bar, it
// says the short of it.
export const ISLAND_WORDY_REM = 56
// How wide a chat must be for its top bar to hold what the island says,
// buttons and all, beside the title, the views and the bar's own buttons;
// narrower, the island takes a row of its own under the bar while it has
// more to say than who is there. At this width the island, giving way eight
// times as fast as the title, still holds its longest pair of buttons.
export const ISLAND_ROW_REM = 50

export type PanelMode = 'push' | 'overlay'

export interface PanelLayout {
  mode: PanelMode
  // The room the chat makes on its right.
  padRem: number
}

// panelLayout decides, for a chat area widthRem wide and a panel panelRem
// wide, whether the panel pushes the chat aside or lies over it, the way
// Feishu does. Pushing, the chat moves only as far as it must: while the
// centred column clears the panel it stays put, and only when the panel
// would cover it does the chat give up room, never more than the panel
// takes.
export function panelLayout(widthRem: number, panelRem: number): PanelLayout {
  const room = panelRem + PANEL_MARGIN_REM
  if (widthRem - room < MIN_CHAT_REM) return { mode: 'overlay', padRem: 0 }
  const overlap = CHAT_COLUMN_REM + 2 * room - widthRem
  return { mode: 'push', padRem: Math.min(room, Math.max(0, overlap)) }
}

// useWidthRem measures an element's width in rem and keeps it current as
// it resizes: set the returned callback as the element's ref. Zero until
// the element is there, which may be a render or two after the component's
// first (a page that shows a loading state first).
export function useWidthRem(): [(element: HTMLElement | null) => void, number] {
  const [element, setElement] = useState<HTMLElement | null>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    if (!element) return
    const measure = () => {
      const rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
      setWidth(element.getBoundingClientRect().width / rootPx)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [element])
  return [setElement, width]
}
