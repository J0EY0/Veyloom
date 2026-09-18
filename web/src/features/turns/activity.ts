import type { TranscriptLine, TurnEvent } from '@/api/types'
import { t } from '@/lib/i18n'

// What a turn did, in the units people read: tools run, files changed,
// permissions asked. Built from live events while the turn runs and from
// the transcript afterwards, so both paths draw the same block.

export type ActivityItem =
  | { kind: 'tool'; tool: string; input: string; result?: string; status: 'running' | 'done' | 'failed' }
  | { kind: 'file'; path: string }
  | { kind: 'approval'; tool: string; input: string; status: string }
  | { kind: 'error'; text: string }

export function activityFromEvents(events: TurnEvent[]): ActivityItem[] {
  const items: ActivityItem[] = []
  const files = new Set<string>()
  for (const event of events) {
    switch (event.kind) {
      case 'tool_call':
        items.push({ kind: 'tool', tool: event.tool ?? '', input: event.input ?? '', status: 'running' })
        break
      case 'tool_result': {
        const open = findLast(items, (item) => item.kind === 'tool' && item.status === 'running' && item.tool === (event.tool ?? item.tool))
        if (open && open.kind === 'tool') {
          open.result = event.text
          open.status = looksFailed(event.text) ? 'failed' : 'done'
        }
        break
      }
      case 'file_changed':
        if (event.path && !files.has(event.path)) {
          files.add(event.path)
          items.push({ kind: 'file', path: event.path })
        }
        break
      case 'approval_request':
        items.push({ kind: 'approval', tool: event.tool ?? '', input: event.input ?? '', status: 'pending' })
        break
      case 'error':
        items.push({ kind: 'error', text: event.text ?? '' })
        break
      default:
        break
    }
  }
  return items
}

export function activityFromTranscript(lines: TranscriptLine[]): ActivityItem[] {
  const events = lines.flatMap((line) => (line.kind === 'event' && line.event ? [line.event] : []))
  const items = activityFromEvents(events)
  // Decisions come as their own lines; settle the pending approvals in order.
  for (const line of lines) {
    if (line.kind !== 'approval_decision' || !line.approval) continue
    const pending = items.find((item) => item.kind === 'approval' && item.status === 'pending')
    if (pending && pending.kind === 'approval') pending.status = line.approval.status
  }
  return items
}

// summarize says what a block holds in one phrase: "改了 2 个文件 · 3 个命令".
export function summarize(items: ActivityItem[]): string {
  const files = items.filter((item) => item.kind === 'file').length
  const tools = items.filter((item) => item.kind === 'tool').length
  const failed = items.filter((item) => item.kind === 'tool' && item.status === 'failed').length
  const approvals = items.filter((item) => item.kind === 'approval').length
  const parts = [
    files > 0 ? t('activity.files', { n: files }) : '',
    tools > 0 ? t('activity.tools', { n: tools }) : '',
    failed > 0 ? t('activity.failures', { n: failed }) : '',
    approvals > 0 ? t('activity.approvals', { n: approvals }) : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : t('activity.none')
}

// describeTool says what a call does in one line, as the hub does for
// approvals: a shell command as itself, anything else as tool and input.
export function describeTool(tool: string, input: string): string {
  if (tool === 'Bash' || tool === 'bash' || tool === 'shell') {
    try {
      const parsed = JSON.parse(input) as { command?: unknown }
      if (typeof parsed.command === 'string') return parsed.command
    } catch {
      // Not JSON: show as is.
    }
    return input || tool
  }
  return input ? `${tool} ${input}` : tool
}

function looksFailed(text: string | undefined): boolean {
  return /^(exit [1-9]|error|failed|fatal)/i.test((text ?? '').trimStart())
}

function findLast<T>(items: T[], match: (item: T) => boolean): T | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    if (match(items[i])) return items[i]
  }
  return undefined
}
