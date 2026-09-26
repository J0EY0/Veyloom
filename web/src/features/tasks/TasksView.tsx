import { useMemo, useState } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useRoomTasks } from '@/api/work'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { useT } from '@/lib/i18n'
import { useNow } from '@/lib/useNow'
import { TaskBoard } from './TaskBoard'
import { WorkView } from './WorkView'

export interface TasksViewProps {
  roomId: string
  // The piece of work open, by the message a person began it with; empty
  // shows the board.
  chain: string
  onOpenThread: (threadId: string) => void
}

// TasksView is the chat's Tasks tab (docs/webui.md 4.20): the board, and
// a piece of work's page when one is open.
export function TasksView({ roomId, chain, onOpenThread }: TasksViewProps) {
  if (chain !== '') return <WorkView roomId={roomId} chain={chain} onOpenThread={onOpenThread} />
  return <Board roomId={roomId} onOpenThread={onOpenThread} />
}

function Board({ roomId, onOpenThread }: { roomId: string; onOpenThread: (threadId: string) => void }) {
  const t = useT()
  const tasks = useRoomTasks(roomId)
  const members = useRoomMembers(roomId)
  const { names, looks } = useMentionTargets(roomId)
  const [params, setParams] = useSearchParams()
  const group = params.get('group') === 'member' ? 'member' : 'state'
  // Members left out of the board, by id.
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set())
  const running = tasks.data?.some((task) => task.state === 'running') ?? false
  const now = useNow(running)
  const current = useMemo(() => (members.data ?? []).map((m) => m.id), [members.data])

  function setGroup(next: string) {
    setParams((prev) => {
      const out = new URLSearchParams(prev)
      if (next === 'member') out.set('group', 'member')
      else out.delete('group')
      return out
    })
  }

  if (tasks.isPending) {
    return (
      <div role="status" aria-label={t('common.loading')} className="grid flex-1 grid-cols-3 gap-3 px-5 pt-14">
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-32 rounded-xl" />
      </div>
    )
  }
  if (tasks.isError) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('tasks.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(tasks.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const shown = tasks.data.filter((task) => !hidden.has(task.member_id))
  // A member taken out of the project keeps a column while it has tasks.
  const columns = [...current, ...new Set(tasks.data.map((task) => task.member_id).filter((id) => !current.includes(id)))].filter((id) => !hidden.has(id))

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-13 flex-none items-center gap-2.5 px-5">
        <Tabs value={group} onValueChange={setGroup}>
          <TabsList aria-label={t('tasks.group')} className="h-8">
            <TabsTrigger value="state" className="px-2.5 text-xs">
              {t('tasks.byState')}
            </TabsTrigger>
            <TabsTrigger value="member" className="px-2.5 text-xs">
              {t('tasks.byMember')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
        <span className="grow" />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" aria-label={t('tasks.filter')} className="gap-1.5 px-1.5">
              <span className="flex items-center -space-x-1.5">
                {columns.slice(0, 4).map((id) => {
                  const look = looks.get(id)
                  return look ? <AgentAvatar key={id} look={look} name={names.get(id)} size="sm" mark={false} className="ring-2 ring-background" /> : null
                })}
              </span>
              <ChevronDownIcon aria-hidden="true" className="text-subtle" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            {current.map((id) => (
              <DropdownMenuCheckboxItem
                key={id}
                checked={!hidden.has(id)}
                onSelect={(event) => event.preventDefault()}
                onCheckedChange={(checked) =>
                  setHidden((prev) => {
                    const next = new Set(prev)
                    if (checked) next.delete(id)
                    else next.add(id)
                    return next
                  })
                }
              >
                {names.get(id) ?? id}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {tasks.data.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('tasks.none')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        <TaskBoard tasks={shown} roomId={roomId} members={columns} names={names} looks={looks} group={group} now={now} onOpenThread={onOpenThread} />
      )}
    </div>
  )
}
