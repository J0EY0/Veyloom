// A piece of text, and whether it is what a search found or a link.
export interface Part {
  text: string
  hit: boolean
}

// markParts cuts text where the words of a search are in it, ignoring
// case as the hub's search does (internal/wiki/search.go): each word
// wherever it is, words overlapping as one.
export function markParts(text: string, query: string): Part[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  const lower = text.toLowerCase()
  // Lowercasing moved the offsets: no marks rather than wrong ones.
  if (words.length === 0 || lower.length !== text.length) return [{ text, hit: false }]
  const found: [number, number][] = []
  for (const word of words) {
    for (let at = lower.indexOf(word); at >= 0; at = lower.indexOf(word, at + word.length)) found.push([at, at + word.length])
  }
  found.sort((a, b) => a[0] - b[0])
  const parts: Part[] = []
  let from = 0
  for (const [start, end] of found) {
    if (end <= from) continue
    const at = Math.max(start, from)
    if (at > from) parts.push({ text: text.slice(from, at), hit: false })
    const last = parts.at(-1)
    if (last?.hit && at === from) last.text += text.slice(at, end)
    else parts.push({ text: text.slice(at, end), hit: true })
    from = end
  }
  if (from < text.length) parts.push({ text: text.slice(from), hit: false })
  return parts
}

// contextParts reads the sentence a link is in as the graph gives it
// (internal/wiki/graph.go): its links written as their text in brackets.
// A link's text stands without the brackets, a page's path as the page's
// title.
export function contextParts(context: string, titleOf: (path: string) => string | undefined): Part[] {
  const parts: Part[] = []
  let from = 0
  for (const match of context.matchAll(/\[([^\]\n]+)\]/g)) {
    const at = match.index ?? 0
    if (at > from) parts.push({ text: context.slice(from, at), hit: false })
    parts.push({ text: titleOf(match[1]) ?? match[1], hit: true })
    from = at + match[0].length
  }
  if (from < context.length) parts.push({ text: context.slice(from), hit: false })
  return parts
}
