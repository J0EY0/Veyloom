import type { TranscriptLine, TurnEvent } from '@/api/types'

// withLive is a running turn's record: its transcript as far as it was
// read, then the events the page heard live after that, told apart by the
// hub's numbers on them.
export function withLive(lines: TranscriptLine[], events: TurnEvent[]): TranscriptLine[] {
  let read = 0
  for (const line of lines) read = Math.max(read, line.event?.seq ?? 0)
  const heard = events.filter((event) => event.seq === undefined || event.seq > read)
  return [...lines, ...heard.map((event): TranscriptLine => ({ kind: 'event', at: event.at, event }))]
}

// drawerLines is what the drawer lists of a turn's record: the session
// bookkeeping left out, and the words the agent streamed in pieces put
// back together, each run of them one line, timed when it began.
export function drawerLines(lines: TranscriptLine[]): TranscriptLine[] {
  const out: TranscriptLine[] = []
  for (const line of lines) {
    const event = line.event
    if (event?.kind === 'session') continue
    const last = out.at(-1)
    if (line.kind === 'event' && event?.kind === 'text' && last?.kind === 'event' && last.event?.kind === 'text') {
      out[out.length - 1] = { ...last, event: { ...last.event, text: (last.event.text ?? '') + (event.text ?? '') } }
      continue
    }
    out.push(line)
  }
  return out
}
