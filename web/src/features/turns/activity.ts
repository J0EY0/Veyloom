import type { TranscriptLine, TurnEvent } from '@/api/types'
import { type AskKind, askKind } from '@/features/approvals/kinds'
import { confirmationOf, planTitle } from '@/features/approvals/plans'
import { t } from '@/lib/i18n'

// What a turn did, in the units people read: tools run, files changed,
// permissions asked. Built from live events while the turn runs and from
// the transcript afterwards, so both paths draw the same block.

export type NoticeLevel = 'info' | 'warning' | 'error'

export type ActivityItem =
  | { kind: 'tool'; tool: string; input: string; result?: string; status: 'running' | 'done' | 'failed' }
  | { kind: 'file'; path: string }
  // id is the runtime's id for the request, which its decision names;
  // reviewer is set when the runtime's own reviewer settled it.
  | { kind: 'approval'; id?: string; tool: string; input: string; status: string; reviewer?: string; asks?: AskKind }
  | { kind: 'error'; text: string }
  // What the runtime told people: shown as it is, never folded away.
  | { kind: 'notice'; level: NoticeLevel; text: string }

export type Notice = Extract<ActivityItem, { kind: 'notice' }>

export function isNotice(item: ActivityItem): item is Notice {
  return item.kind === 'notice'
}

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
        items.push({
          kind: 'approval',
          id: event.approval_id,
          tool: event.tool ?? '',
          input: event.input ?? '',
          // Settled by the runtime's own reviewer: nothing is pending.
          status: event.reviewer ? (event.verdict ?? 'allowed') : 'pending',
          reviewer: event.reviewer,
          asks: event.approval_kind ? askKind(event.approval_kind) : undefined,
        })
        break
      case 'notice':
        items.push({ kind: 'notice', level: noticeLevel(event.level), text: event.text ?? '' })
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
  // Decisions come as their own lines, naming the request they settle; one
  // without a name settles the oldest request still pending.
  for (const line of lines) {
    if (line.kind !== 'approval_decision' || !line.approval) continue
    const { request_id: requestId, status } = line.approval
    const settled =
      items.find((item) => item.kind === 'approval' && Boolean(requestId) && item.id === requestId) ??
      items.find((item) => item.kind === 'approval' && item.status === 'pending')
    if (settled && settled.kind === 'approval') settled.status = status
  }
  return items
}

// summarize says what a block holds in one phrase: "改了 2 个文件 · 3 个命令".
export function summarize(items: ActivityItem[]): string {
  const files = items.filter((item) => item.kind === 'file').length
  const tools = items.filter((item) => item.kind === 'tool').length
  const failed = items.filter((item) => item.kind === 'tool' && item.status === 'failed').length
  const asked = (kind: AskKind) => items.filter((item) => item.kind === 'approval' && (item.asks ?? 'tool_use') === kind).length
  const approvals = asked('tool_use')
  const questions = asked('question')
  const requests = asked('form') + asked('link')
  const parts = [
    files > 0 ? t('activity.files', { n: files }) : '',
    tools > 0 ? t('activity.tools', { n: tools }) : '',
    failed > 0 ? t('activity.failures', { n: failed }) : '',
    approvals > 0 ? t('activity.approvals', { n: approvals }) : '',
    questions > 0 ? t('activity.questions', { n: questions }) : '',
    requests > 0 ? t('activity.asks', { n: requests }) : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : t('activity.none')
}

// describeTool says what a call does in one line, as the hub does for
// approvals: a shell command as itself, anything else as tool and input.
export function describeTool(tool: string, input: string): string {
  if (tool === 'Bash' || tool === 'bash' || tool === 'shell' || tool === 'commandExecution') {
    try {
      const parsed = JSON.parse(input) as { command?: unknown }
      if (typeof parsed.command === 'string') return parsed.command
    } catch {
      // Not JSON: show as is.
    }
    return input || tool
  }
  if (tool === 'ExitPlanMode' || tool === 'SandboxNetworkAccess') {
    let parsed: { plan?: unknown; host?: unknown } = {}
    try {
      parsed = JSON.parse(input) as typeof parsed
    } catch {
      // Not JSON: fall through to the tool and its input.
    }
    if (tool === 'ExitPlanMode' && typeof parsed.plan === 'string') return t('tool.plan', { title: planTitle(parsed.plan) || t('plan.untitled') })
    if (tool === 'SandboxNetworkAccess' && typeof parsed.host === 'string') return t('tool.network', { host: parsed.host })
  }
  if (tool === 'confirm') {
    const confirmation = confirmationOf({ tool, input })
    if (confirmation) return t('tool.confirm', { text: [confirmation.title, confirmation.message].filter(Boolean).join(' — ') })
  }
  return input ? `${tool} ${input}` : tool
}

function noticeLevel(level: string | undefined): NoticeLevel {
  return level === 'warning' || level === 'error' ? level : 'info'
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
