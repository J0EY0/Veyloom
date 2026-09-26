import { useState } from 'react'
import { BotIcon, ChevronRightIcon, PlusIcon, SearchIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useRoomMembers } from '@/api/agents'
import { useProject, useUpdateProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { Member, Project } from '@/api/types'
import { SidePanel } from '@/components/layout/SidePanel'
import { ProjectMark } from '@/components/shared/project-mark'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Item, ItemContent, ItemDescription, ItemGroup, ItemMedia, ItemTitle } from '@/components/ui/item'
import { Spinner } from '@/components/ui/spinner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { EditProjectDialog } from '@/features/projects/EditProjectDialog'
import { byStatus } from '@/features/rooms/memberStatus'
import { useMemberStates } from '@/features/rooms/useMemberStates'
import { AddMemberDialog } from './AddMemberDialog'
import { EditMemberDialog } from './EditMemberDialog'
import { MemberRow } from './MemberRow'
import { RemoveMemberDialog } from './RemoveMemberDialog'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

// From this many members on the list gets a search box; fewer are read at
// a glance.
const SEARCH_FROM = 6

export interface MembersPanelProps {
  roomId: string
  roomName: string
  onClose: () => void
  onOpenThread: (threadId: string) => void
}

// The chat's info (2026-09-17), after Feishu's group settings: the project
// the chat belongs to, then who is in it, beside the chat instead of on a
// page of its own. Each member row says what it is doing right now, and
// what you do to it: edit, switch off, take out. The team's files and
// wikis are to come here as entries.
export function MembersPanel({ roomId, roomName, onClose, onOpenThread }: MembersPanelProps) {
  const t = useT()
  const project = useProject(useRoom(roomId).data?.project_id ?? '')
  // The same two queries the room already holds; react-query hands back
  // what is cached rather than asking again.
  const members = useRoomMembers(roomId)
  const states = useMemberStates(roomId)
  const update = useUpdateProject(project?.id ?? '')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Member>()
  const [removing, setRemoving] = useState<Member>()
  const [editingProject, setEditingProject] = useState(false)
  const [query, setQuery] = useState('')
  const sorted = [...states].sort((a, b) => byStatus(a.status, b.status))
  const needle = query.trim().toLowerCase()
  const shown = needle ? sorted.filter((state) => state.member.display_name.toLowerCase().includes(needle)) : sorted

  function makeLeader(member: Member) {
    update.mutate(
      { leader_member_id: member.id },
      {
        onSuccess: () => toast.success(t('member.madeLeader', { name: member.display_name })),
        onError: (err) => toast.error(t('member.makeLeaderFailed', { error: errorText(err) })),
      },
    )
  }

  return (
    <SidePanel
      label={t('room.info')}
      header={<h2 className="text-sm font-semibold">{t('room.info')}</h2>}
      narrow
      onClose={onClose}
      closeLabel={t('room.infoClose')}
    >
      {/* At least as tall as the panel, so a note in place of the members
          takes the rest of it and sits in the middle. */}
      <div className="flex min-h-full flex-col gap-5">
        {project ? <ProjectCard project={project} onEdit={() => setEditingProject(true)} /> : null}
        <section aria-label={t('members.title')} className="flex flex-1 flex-col gap-1.5">
          <div className="flex h-7 items-center gap-1.5">
            <h3 className="text-xs font-medium text-subtle">{t('members.title')}</h3>
            {states.length > 0 ? <span className="text-xs text-subtle tabular-nums">{states.length}</span> : null}
            <span className="grow" />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t('members.add')}
                  onClick={() => setAdding(true)}
                  className="text-subtle hover:text-foreground"
                >
                  <PlusIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('members.add')}</TooltipContent>
            </Tooltip>
          </div>
          {sorted.length >= SEARCH_FROM ? (
            <InputGroup className="mb-1 h-8">
              <InputGroupAddon>
                <SearchIcon />
              </InputGroupAddon>
              <InputGroupInput
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t('members.search')}
                aria-label={t('members.search')}
                className="[&::-webkit-search-cancel-button]:appearance-none"
              />
            </InputGroup>
          ) : null}
          {members.isPending ? (
            <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
              <Spinner className="size-3" />
              {t('common.loading')}
            </p>
          ) : members.isError ? (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>{t('members.failed')}</EmptyTitle>
                <EmptyDescription>{errorText(members.error)}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : sorted.length === 0 ? (
            <Empty className="gap-4 px-0 md:px-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BotIcon />
                </EmptyMedia>
                <EmptyTitle>{t('members.empty')}</EmptyTitle>
              </EmptyHeader>
              <EmptyContent>
                <Button size="sm" onClick={() => setAdding(true)}>
                  {t('members.add')}
                </Button>
              </EmptyContent>
            </Empty>
          ) : shown.length === 0 ? (
            <p className="py-6 text-center text-xs text-subtle">{t('members.noMatch')}</p>
          ) : (
            <ItemGroup className="-mx-1">
              {shown.map((state) => (
                <MemberRow
                  key={state.member.id}
                  roomId={roomId}
                  state={state}
                  leader={state.member.id === project?.leader_id}
                  onEdit={setEditing}
                  onMakeLeader={makeLeader}
                  onRemove={setRemoving}
                  onOpenThread={onOpenThread}
                />
              ))}
            </ItemGroup>
          )}
        </section>
      </div>
      {adding ? <AddMemberDialog roomId={roomId} roomName={roomName} open onClose={() => setAdding(false)} /> : null}
      {editing ? <EditMemberDialog roomId={roomId} member={editing} onClose={() => setEditing(undefined)} /> : null}
      {editingProject && project ? <EditProjectDialog project={project} onClose={() => setEditingProject(false)} /> : null}
      {removing ? <RemoveMemberDialog roomId={roomId} roomName={roomName} member={removing} onClose={() => setRemoving(undefined)} /> : null}
    </SidePanel>
  )
}

// The project this chat belongs to: its mark, its name and where it is
// checked out, which is where its members work. The card opens the
// project for editing, the way a member's row opens the member.
function ProjectCard({ project, onEdit }: { project: Project; onEdit: () => void }) {
  const t = useT()
  return (
    <Item asChild size="sm" className="-mx-1 w-[calc(100%+0.5rem)] flex-nowrap gap-3 rounded-lg border-0 px-1 py-1 text-left hover:bg-muted">
      <button type="button" aria-label={`${t('project.edit')} ${project.name}`} onClick={onEdit}>
        <ItemMedia>
          <ProjectMark name={project.name} size="lg" />
        </ItemMedia>
        <ItemContent className="min-w-0 gap-0.5">
          <ItemTitle className="block max-w-full truncate text-sm font-semibold">{project.name}</ItemTitle>
          {project.repo_path ? (
            <ItemDescription className="font-mono text-[0.71875rem] leading-snug break-all text-subtle" translate="no">
              {project.repo_path}
            </ItemDescription>
          ) : null}
        </ItemContent>
        <ChevronRightIcon aria-hidden="true" className="size-4 flex-none text-subtle" />
      </button>
    </Item>
  )
}
