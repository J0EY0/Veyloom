// withoutTitleHeading drops the heading a page's text opens with when it
// only says the page's title again, which the page shows above it anyway:
// a skill's SKILL.md, or a page written by hand, often starts that way.
export function withoutTitleHeading(body: string, title: string): string {
  const match = /^\s*#[ \t]+(.+?)[ \t]*#*[ \t]*(?:\n|$)/.exec(body)
  if (!match || title.trim() === '' || match[1].trim() !== title.trim()) return body
  return body.slice(match[0].length).replace(/^\s*\n/, '')
}
