import { useState, type CSSProperties, type ReactNode } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import type { Task, TaskState } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusMark } from '@/components/shared/status-mark'
import { Button } from '@/components/ui/button'
import type { AgentLook } from '@/lib/agentLooks'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { TaskCard } from './TaskCard'
import { byMember, byState, countStates, shownDone, taskStates } from './tasks'

export interface TaskBoardProps {
  tasks: Task[]
  roomId: string
  // The members whose columns the board has when grouped by member, in
  // order; their names and looks.
  members: string[]
  names: ReadonlyMap<string, string>
  looks: ReadonlyMap<string, AgentLook>
  group: 'state' | 'member'
  now: number
  onOpenThread: (threadId: string) => void
}

// Column tints: the state's colour, barely there.
const tints: Record<TaskState, string> = {
  running: 'bg-status-run/[0.05]',
  merge: 'bg-status-merge/[0.05]',
  done: 'bg-status-ok/[0.04]',
}

// TaskBoard lays the tasks out in columns (docs/webui.md 4.20): by state,
// under way, waiting to be merged, done; or a column a member. Done tasks
// past the latest few fold away.
export function TaskBoard(props: TaskBoardProps) {
  return props.group === 'member' ? <MemberColumns {...props} /> : <StateColumns {...props} />
}

function StateColumns({ tasks, roomId, names, looks, now, onOpenThread }: TaskBoardProps) {
  const t = useT()
  const columns = byState(tasks, now)
  return (
    <Lanes columns={3}>
      {taskStates.map((state) => {
        const label = t(`tasks.state.${state}`)
        return (
          <section key={state} aria-label={label} className={cn('flex min-w-0 flex-col gap-2 rounded-xl px-2 pt-1.5 pb-2', tints[state])}>
            <h2 className="flex h-7 items-center gap-2 px-1 text-[0.8125rem] font-medium">
              <StatusMark kind={state === 'running' ? 'run' : state} />
              {label}
              <span className="font-normal text-subtle tabular-nums">{columns[state].length}</span>
            </h2>
            <Cards tasks={columns[state]} fold={state === 'done'} {...{ roomId, names, looks, now, onOpenThread }} />
          </section>
        )
      })}
    </Lanes>
  )
}

// Lanes are the board's columns, as tall as the board, side by side on a
// wide screen and one above the other on a narrow one; the board scrolls.
function Lanes({ columns, children }: { columns: number; children: ReactNode }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
      <div
        className="grid min-h-full grid-cols-1 items-stretch gap-3 md:grid-cols-[repeat(var(--columns),minmax(0,1fr))]"
        style={{ '--columns': Math.max(columns, 1) } as CSSProperties}
      >
        {children}
      </div>
    </div>
  )
}

function MemberColumns({ tasks, roomId, members, names, looks, now, onOpenThread }: TaskBoardProps) {
  const t = useT()
  const columns = byMember(tasks, members, now)
  return (
    <Lanes columns={members.length}>
      {members.map((id) => {
        const column = columns.get(id) ?? []
        const name = names.get(id) ?? t('sender.unknownMember')
        const look = looks.get(id)
        return (
          <section key={id} aria-label={name} className="flex min-w-0 flex-col gap-2 rounded-xl bg-muted/60 px-2 pt-1.5 pb-2">
            <div className="flex h-7.5 items-center gap-2 px-1">
              {look ? <AgentAvatar look={look} name={name} size="sm" mark={false} /> : null}
              <h2 className="min-w-0 truncate text-[0.8125rem] font-medium">{name}</h2>
              <span className="grow" />
              {countStates(column).map(({ state, kind, n }) => (
                <span key={state} className="flex items-center gap-1 text-xs text-subtle tabular-nums">
                  <StatusMark kind={kind} className="size-3" />
                  <span className="sr-only">{t(`tasks.state.${state}`)}</span>
                  {n}
                </span>
              ))}
            </div>
            <Cards tasks={column} fold={false} byMember {...{ roomId, names, looks, now, onOpenThread }} />
          </section>
        )
      })}
    </Lanes>
  )
}

interface CardsProps {
  tasks: Task[]
  roomId: string
  names: ReadonlyMap<string, string>
  looks: ReadonlyMap<string, AgentLook>
  now: number
  onOpenThread: (threadId: string) => void
  // Past the latest few, the rest fold away behind a button.
  fold: boolean
  byMember?: boolean
}

function Cards({ tasks, fold, byMember, roomId, names, looks, now, onOpenThread }: CardsProps) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const shown = fold && !open ? tasks.slice(0, shownDone) : tasks
  const unknown = t('sender.unknownMember')
  return (
    <>
      {shown.map((task) => (
        <TaskCard
          key={`${task.chain}/${task.thread_id}/${task.member_id}`}
          task={task}
          roomId={roomId}
          name={names.get(task.member_id) ?? unknown}
          look={looks.get(task.member_id)}
          byMember={byMember}
          now={now}
          onOpenThread={onOpenThread}
        />
      ))}
      {fold && tasks.length > shownDone ? (
        <Button variant="ghost" size="sm" aria-expanded={open} onClick={() => setOpen((value) => !value)} className="text-xs font-normal text-subtle">
          {open ? t('tasks.fewer') : t('tasks.earlier', { n: tasks.length - shownDone })}
          <ChevronDownIcon aria-hidden="true" className={cn('transition-transform', open && 'rotate-180')} />
        </Button>
      ) : null}
    </>
  )
}
