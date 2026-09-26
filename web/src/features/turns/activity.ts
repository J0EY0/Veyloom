import type { Approval, TranscriptLine, TurnEvent } from '@/api/types'
import { type AskKind, askKind } from '@/features/approvals/kinds'
import { confirmationOf, planTitle } from '@/features/approvals/plans'
import { noticeText } from '@/features/threads/systemNote'
import { t } from '@/lib/i18n'

// What a turn did, in the units people read: tools run, files changed,
// permissions asked. Built from live events while the turn runs and from
// the transcript afterwards, so both paths draw the same block.

export type NoticeLevel = 'info' | 'warning' | 'error'

export type ActivityItem =
  // A call waiting is held up by a request for a person's permission.
  | { kind: 'tool'; tool: string; input: string; result?: string; status: 'running' | 'waiting' | 'done' | 'failed' }
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

// activityFromEvents reads a turn's events; ended says the turn is over,
// so that what was under way as it began reads as done.
export function activityFromEvents(events: TurnEvent[], ended = false): ActivityItem[] {
  const items: ActivityItem[] = []
  const files = new Set<string>()
  for (const [index, event] of events.entries()) {
    switch (event.kind) {
      case 'tool_call':
        items.push({ kind: 'tool', tool: event.tool ?? '', input: event.input ?? '', status: 'running' })
        break
      case 'tool_result': {
        const open = findLast(
          items,
          (item) => item.kind === 'tool' && (item.status === 'running' || item.status === 'waiting') && item.tool === (event.tool ?? item.tool),
        )
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
        // Something happened after it: what it said was under way is done.
        items.push({ kind: 'notice', level: noticeLevel(event.level), text: noticeText(t, event.text ?? '', ended || index < events.length - 1) })
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

// withApprovals brings a running turn's steps up to date from the requests
// the hub recorded of it, which its live events do not carry: how each was
// settled, and the call a request still waiting holds up. That call is the
// one with the same command, else the latest of the same tool still going.
export function withApprovals(items: ActivityItem[], approvals: Pick<Approval, 'request_id' | 'status' | 'kind' | 'tool' | 'input'>[]): ActivityItem[] {
  const status = new Map(approvals.map((a) => [a.request_id, a.status]))
  const next = items.map((item): ActivityItem => {
    if (item.kind === 'approval' && item.id !== undefined && status.has(item.id)) return { ...item, status: status.get(item.id) as string }
    return item.kind === 'tool' && item.status === 'waiting' ? { ...item, status: 'running' } : item
  })
  for (const approval of approvals) {
    if (approval.status !== 'pending' || approval.kind !== 'tool_use') continue
    const going = (item: ActivityItem) => item.kind === 'tool' && item.status === 'running' && item.tool === approval.tool
    const command = commandOf(approval.input)
    const held =
      findLast(next, (item) => going(item) && item.kind === 'tool' && command !== undefined && commandOf(item.input) === command) ?? findLast(next, going)
    if (held?.kind === 'tool') held.status = 'waiting'
  }
  return next
}

// commandOf is the shell command of a tool call's input, JSON as text or
// already read; undefined for any other input.
function commandOf(input: unknown): string | undefined {
  let value = input
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value)
    } catch {
      return undefined
    }
  }
  const command = (value as { command?: unknown } | null)?.command
  return typeof command === 'string' ? command : undefined
}

export function activityFromTranscript(lines: TranscriptLine[]): ActivityItem[] {
  const events = lines.flatMap((line) => (line.kind === 'event' && line.event ? [line.event] : []))
  const items = activityFromEvents(events, true)
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

// What a tool is for, whichever runtime's spelling it has: what a summary
// counts it as. Reaching for a tool (Claude Code's ToolSearch) is none.
type ToolSort = 'wiki' | 'room' | 'read' | 'search' | 'command' | 'edit' | 'send' | 'memory' | 'reach' | 'other'

const sorts: Record<string, ToolSort> = {
  Bash: 'command',
  bash: 'command',
  shell: 'command',
  commandExecution: 'command',
  exec_command: 'command',
  Read: 'read',
  read: 'read',
  read_file: 'read',
  view: 'read',
  Glob: 'search',
  Grep: 'search',
  glob: 'search',
  grep: 'search',
  find: 'search',
  ls: 'search',
  LS: 'search',
  WebSearch: 'search',
  WebFetch: 'search',
  web_search: 'search',
  Edit: 'edit',
  edit: 'edit',
  Write: 'edit',
  write: 'edit',
  MultiEdit: 'edit',
  NotebookEdit: 'edit',
  apply_patch: 'edit',
  write_file: 'edit',
  fileChange: 'edit',
  list_topics: 'room',
  read_topic: 'room',
  read_turn: 'room',
  read_room: 'room',
  search_messages: 'room',
  send_message: 'send',
  remember: 'memory',
  forget: 'memory',
  ToolSearch: 'reach',
}

function sortOf(tool: string): ToolSort {
  const name = tool.replace(/^(?:mcp__veyloom__|veyloom[./])/, '')
  return sorts[name] ?? (/wiki/.test(name) ? 'wiki' : 'other')
}

// summarize says what a block holds in one phrase, by what the turn did:
// "查了 wiki、读了 2 个文件、派出 1 条消息".
export function summarize(items: ActivityItem[]): string {
  const count = (sort: ToolSort) => items.filter((item) => item.kind === 'tool' && sortOf(item.tool) === sort).length
  const files = items.filter((item) => item.kind === 'file').length
  const failed = items.filter((item) => item.kind === 'tool' && item.status === 'failed').length
  const asked = (kind: AskKind) => items.filter((item) => item.kind === 'approval' && (item.asks ?? 'tool_use') === kind).length
  const n = {
    wiki: count('wiki'),
    room: count('room'),
    read: count('read'),
    search: count('search'),
    command: count('command'),
    send: count('send'),
    memory: count('memory'),
    other: count('other'),
  }
  const parts = [
    n.wiki > 0 ? t('activity.wiki') : '',
    n.room > 0 ? t('activity.room') : '',
    n.read > 0 ? t('activity.reads', { n: n.read }) : '',
    n.search > 0 ? t('activity.searches', { n: n.search }) : '',
    n.command > 0 ? t('activity.commands', { n: n.command }) : '',
    files > 0 ? t('activity.files', { n: files }) : '',
    n.send > 0 ? t('activity.sent', { n: n.send }) : '',
    n.memory > 0 ? t('activity.memories', { n: n.memory }) : '',
    n.other > 0 ? t('activity.tools', { n: n.other }) : '',
    failed > 0 ? t('activity.failures', { n: failed }) : '',
    asked('tool_use') > 0 ? t('activity.approvals', { n: asked('tool_use') }) : '',
    asked('question') > 0 ? t('activity.questions', { n: asked('question') }) : '',
    asked('form') + asked('link') > 0 ? t('activity.asks', { n: asked('form') + asked('link') }) : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(t('common.listSeparator')) : t('activity.none')
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
  // Records of old, and some tests, can lack either.
  const name = (tool ?? '').replace(/^mcp__veyloom__/, '')
  const subject = subjectOf(input ?? '')
  return subject ? `${name} ${subject}` : name
}

// The fields of a tool's input that say what it works on, by the names the
// runtimes give them, most telling first.
const subjectFields = ['file_path', 'notebook_path', 'path', 'paths', 'pattern', 'query', 'url', 'slug', 'title', 'topic']

// subjectOf is what a tool's input says the call works on, for one line:
// a file, what it searches for, a page; else the input as it came. A long
// absolute path keeps its last parts.
function subjectOf(input: string): string {
  let parsed: unknown
  try {
    parsed = JSON.parse(input)
  } catch {
    return input
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return input
  const fields = parsed as Record<string, unknown>
  for (const key of subjectFields) {
    const value = fields[key]
    if (typeof value === 'string' && value !== '') return shortPath(value)
    if (Array.isArray(value) && value.length > 0 && value.every((v) => typeof v === 'string')) return value.map(shortPath).join(' ')
  }
  return input === '{}' ? '' : input
}

// shortPath keeps the last three parts of an absolute path.
function shortPath(value: string): string {
  if (!value.startsWith('/')) return value
  const parts = value.split('/').filter(Boolean)
  return parts.length > 3 ? '…/' + parts.slice(-3).join('/') : value
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
