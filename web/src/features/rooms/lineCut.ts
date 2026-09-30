// A line that ends within this much past the limit is still in, and a piece
// of a line reaches past the cut only by more (subpixel layout).
const slack = 0.5

interface Box {
  top: number
  bottom: number
}

// lineCut is how far down an element, in pixels from its top and at most
// limit, to cut its content short without cutting through a line of text:
// the bottom of the lowest line that ends by the limit, above any taller
// piece of that line (a mention, bigger type) reaching past it. Where no
// line ends by the limit, or the browser cannot say where its lines are,
// the limit itself.
export function lineCut(element: HTMLElement, limit: number): number {
  const range = document.createRange()
  if (typeof range.getClientRects !== 'function') return limit
  const top = element.getBoundingClientRect().top
  const boxes: Box[] = []
  const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
  // Lines come in the order they read: past the limit, nothing more counts.
  scan: for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!node.textContent?.trim()) continue
    range.selectNodeContents(node)
    for (const rect of Array.from(range.getClientRects())) {
      if (rect.height === 0) continue
      const box = { top: rect.top - top, bottom: rect.bottom - top }
      if (box.top >= limit) break scan
      boxes.push(box)
    }
  }
  let cut = 0
  for (const box of boxes) if (box.bottom <= limit + slack && box.bottom > cut) cut = box.bottom
  for (let over = crossing(boxes, cut); over; over = crossing(boxes, cut)) cut = over.top
  return cut > 0 ? cut : limit
}

// crossing is a box the cut would go through.
function crossing(boxes: Box[], cut: number): Box | undefined {
  return boxes.find((box) => box.top < cut - slack && box.bottom > cut + slack)
}
