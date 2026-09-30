// plainText is markdown as one line of words, its syntax taken off: for a
// list's excerpt, a card's title or a quoted reason, where what a person
// or an agent wrote is shown without being rendered. A field the hub left
// out, being empty, has no words.
export function plainText(markdown: string | undefined): string {
  if (!markdown) return ''
  return markdown
    .replace(/^\s*(```|~~~).*$/gm, ' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s*(?:[-*+]|\d+[.)])\s+/gm, '')
    .replace(/^\s*[-*_|: ]{3,}\s*$/gm, ' ')
    .replace(/(\*\*|__|~~|`)/g, '')
    .replace(/(^|[\s(])[*_](\S(?:.*?\S)?)[*_](?=[\s).,;:!?，。；：！？]|$)/gm, '$1$2')
    .replace(/\s*\|\s*/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}
