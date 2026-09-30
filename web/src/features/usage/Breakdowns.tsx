import type { ReactNode } from 'react'
import { Link } from 'react-router'
import type { Usage } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { useAgentLooks } from '@/lib/agentLooks'
import { formatCompactCount } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { totalTokens } from '@/lib/tokens'
import { cn } from '@/lib/utils'
import { shareOf } from './usage'

// A runtime's own colour, the one place on the page it appears.
const runtimeColours: Record<string, string> = {
  claude: 'bg-runtime-claude',
  codex: 'bg-runtime-codex',
  pi: 'bg-runtime-pi',
}

// Card is one of the usage page's cards: a heading, and what it holds. Its
// rows lay out by how wide the card is, not the screen.
export function Card({ title, aside, children, className }: { title: string; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section aria-label={title} className={cn('@container flex min-w-0 flex-col gap-3.5 rounded-xl border bg-card px-4.5 pt-3.5 pb-4', className)}>
      <div className="flex h-6.5 items-center gap-3">
        <h2 className="grow text-[0.8125rem] font-medium">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  )
}

// Runtimes shares the tokens out by runtime (docs/webui.md 4.20): a bar in
// parts, each part its runtime's colour and its share written over it, the
// runtimes by name with their turns and tokens right below. A part too
// narrow for its share goes without rather than show it cut.
export function Runtimes({ usage }: { usage: Usage }) {
  const t = useT()
  const total = totalTokens(usage.total)
  return (
    <Card title={t('usage.runtimes')}>
      <div className="flex flex-col gap-2.5">
        <div className="flex gap-0.5">
          {usage.runtimes.map((r) => (
            <span key={r.runtime} className="@container min-w-0" style={{ width: `${shareOf(r.tokens, total) * 100}%` }}>
              <span className="hidden text-xl font-semibold tracking-[-0.01em] whitespace-nowrap @min-[2.75rem]:block">
                {Math.round(shareOf(r.tokens, total) * 100)}%
              </span>
            </span>
          ))}
        </div>
        <div aria-hidden="true" className="flex h-2.5 gap-0.5">
          {usage.runtimes.map((r) => (
            <span
              key={r.runtime}
              className={cn('h-full rounded-[0.1875rem]', runtimeColours[r.runtime] ?? 'bg-chart-mark')}
              style={{ width: `${shareOf(r.tokens, total) * 100}%` }}
            />
          ))}
        </div>
      </div>
      <ul className="mt-1 flex flex-col">
        {usage.runtimes.map((r) => (
          <li key={r.runtime} className="grid h-8 grid-cols-[0.625rem_minmax(0,1fr)_auto_auto] items-center gap-x-3 border-t text-[0.8125rem]">
            <span aria-hidden="true" className={cn('size-2 rounded-[0.125rem]', runtimeColours[r.runtime] ?? 'bg-chart-mark')} />
            <span>{runtimeName(r.runtime)}</span>
            <span className="text-xs text-subtle tabular-nums">{t('usage.turnCount', { n: r.turns })}</span>
            <span className="min-w-14 text-right tabular-nums">{formatCompactCount(r.tokens)}</span>
          </li>
        ))}
      </ul>
    </Card>
  )
}

// Bar is a share of the most, the first in blue.
function Bar({ share, first }: { share: number; first: boolean }) {
  return (
    <span aria-hidden="true" className="block h-1.5 overflow-hidden rounded-full bg-chart-track">
      <span className={cn('block h-full rounded-full', first ? 'bg-chart-accent' : 'bg-chart-mark')} style={{ width: `${share * 100}%` }} />
    </span>
  )
}

// Members ranks the members by what they spent: who they are and run, a
// bar against the most, the tokens and their share of all.
export function Members({ usage, projects }: { usage: Usage; projects: boolean }) {
  const t = useT()
  const lookOf = useAgentLooks()
  const total = totalTokens(usage.total)
  const top = usage.members[0]?.tokens ?? 0
  return (
    <Card title={t('usage.members')}>
      <ol className="flex flex-col">
        {usage.members.map((m, index) => (
          <li
            key={m.member_id}
            className="grid h-12 grid-cols-[1.75rem_minmax(0,1fr)_minmax(0,1fr)_3.5rem] items-center gap-x-3 @sm:grid-cols-[1.75rem_minmax(0,6.5rem)_minmax(0,1fr)_3.5rem_2.25rem]"
          >
            <AgentAvatar look={lookOf(m.agent_id) ?? { avatar: '', runtime: m.runtime }} name={m.name} size="md" mark={false} />
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-[0.8125rem] font-medium">{m.name}</span>
              <span className="truncate font-mono text-[0.71875rem] text-subtle">{projects ? m.project_name : m.model || runtimeName(m.runtime)}</span>
            </span>
            <Bar share={shareOf(m.tokens, top)} first={index === 0} />
            <span className="text-right text-[0.8125rem] tabular-nums">{formatCompactCount(m.tokens)}</span>
            <span className="hidden text-right text-xs text-subtle tabular-nums @sm:block">{Math.round(shareOf(m.tokens, total) * 100)}%</span>
          </li>
        ))}
      </ol>
    </Card>
  )
}

// Works ranks the pieces of work by what they spent; a project's setup
// and its wiki's upkeep are there too, quieter.
export function Works({ usage, projects }: { usage: Usage; projects: boolean }) {
  const t = useT()
  const top = usage.works[0]?.tokens ?? 0
  return (
    <Card title={t('usage.works')} aside={<span className="font-mono text-xs text-subtle">{usage.works.length}</span>}>
      <ol className="flex flex-col">
        {usage.works.map((w, index) => {
          const system = w.kind !== 'chat'
          const title = system ? t(w.kind === 'setup' ? 'usage.setup' : 'usage.upkeep') : w.title || t('tasks.untitled', { n: w.thread_number ?? 0 })
          return (
            <li
              key={`${w.chain ?? ''}/${w.room_id}/${w.kind}`}
              className="grid h-7.75 grid-cols-[2rem_minmax(0,3fr)_minmax(0,2fr)_3.5rem] items-center gap-x-3 @sm:grid-cols-[2.25rem_minmax(0,1fr)_7.5rem_3.5rem]"
            >
              <span className="font-mono text-[0.71875rem] text-subtle">{w.thread_number ? `#${w.thread_number}` : ''}</span>
              <span className="flex min-w-0 items-baseline gap-2">
                {w.chain ? (
                  <Link to={`/rooms/${w.room_id}/tasks/${w.chain}`} className="min-w-0 truncate text-[0.8125rem] hover:underline">
                    {title}
                  </Link>
                ) : (
                  <span className="min-w-0 truncate text-[0.8125rem] text-subtle">{title}</span>
                )}
                {projects ? <span className="flex-none text-xs text-subtle">{w.project_name}</span> : null}
              </span>
              <Bar share={shareOf(w.tokens, top)} first={index === 0} />
              <span className="text-right text-[0.8125rem] tabular-nums">{formatCompactCount(w.tokens)}</span>
            </li>
          )
        })}
      </ol>
    </Card>
  )
}
