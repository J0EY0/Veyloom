import { useState } from 'react'
import { EllipsisIcon, PencilIcon, PlusIcon, Trash2Icon } from 'lucide-react'
import { Link, useLocation, useMatch } from 'react-router'
import { useProjects } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import { useRunningTopics } from '@/api/topics'
import type { Project, RunningTopic } from '@/api/types'
import { StatusDot } from '@/components/shared/status-dot'
import { ProjectMark } from '@/components/shared/project-mark'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import {
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { DeleteProjectDialog } from '@/features/projects/DeleteProjectDialog'
import { EditProjectDialog } from '@/features/projects/EditProjectDialog'
import { NewProjectDialog } from '@/features/projects/NewProjectDialog'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

// The projects, one row each. A project is its group chat, so a row opens
// the chat; the one whose chat is on screen is marked. Under a project
// hang the topics agents are working on in it right now.
export function SidebarProjects() {
  const projects = useProjects()
  const topics = useRunningTopics()
  const [creating, setCreating] = useState(false)
  const [renaming, setRenaming] = useState<Project>()
  const [deleting, setDeleting] = useState<Project>()
  const t = useT()
  // Which project the chat on screen belongs to, and which topic is open.
  const match = useMatch('/rooms/:roomId/*')
  const room = useRoom(match?.params.roomId ?? '')
  const currentProject = room.data?.project_id ?? ''
  const openThread = new URLSearchParams(useLocation().search).get('thread') ?? ''

  return (
    <SidebarGroup className="py-1">
      {/* The + belongs to the title row: it shows while that row is hovered, not the projects under it. */}
      <div className="group/projects-title relative">
        <SidebarGroupLabel className="h-7 text-[0.78125rem] text-subtle">{t('nav.projects')}</SidebarGroupLabel>
        <Tooltip>
          <TooltipTrigger asChild>
            <SidebarGroupAction
              aria-label={t('nav.newProject')}
              onClick={() => setCreating(true)}
              className="top-1 right-0.5 text-subtle opacity-0 transition-opacity group-hover/projects-title:opacity-100 focus-visible:opacity-100"
            >
              <PlusIcon />
            </SidebarGroupAction>
          </TooltipTrigger>
          <TooltipContent side="right">{t('nav.newProject')}</TooltipContent>
        </Tooltip>
      </div>
      <SidebarGroupContent>
        {projects.isPending ? (
          <SidebarMenu role="status" aria-label={t('common.loading')}>
            <SidebarMenuSkeleton className="h-7" />
            <SidebarMenuSkeleton className="h-7" />
          </SidebarMenu>
        ) : projects.isError ? (
          <p role="alert" className="px-2 py-1 text-xs text-status-fail">
            {t('nav.projectsFailed', { error: errorText(projects.error) })}
          </p>
        ) : projects.data.length === 0 ? (
          // No projects yet: the way to the first one, in plain sight.
          <SidebarMenu aria-label={t('nav.projects')}>
            <SidebarMenuItem>
              <SidebarMenuButton onClick={() => setCreating(true)} className="h-7 text-[0.8125rem] text-muted-foreground">
                <PlusIcon className="text-subtle" />
                <span>{t('nav.newProject')}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        ) : (
          <SidebarMenu aria-label={t('nav.projects')}>
            {projects.data.map((project) => (
              <ProjectItem
                key={project.id}
                project={project}
                active={project.id === currentProject}
                topics={(topics.data ?? []).filter((topic) => topic.room_id === project.main_room_id)}
                openThread={openThread}
                onRename={setRenaming}
                onDelete={setDeleting}
              />
            ))}
          </SidebarMenu>
        )}
      </SidebarGroupContent>
      {creating ? <NewProjectDialog open onClose={() => setCreating(false)} /> : null}
      {renaming ? <EditProjectDialog project={renaming} rename onClose={() => setRenaming(undefined)} /> : null}
      {deleting ? <DeleteProjectDialog project={deleting} onClose={() => setDeleting(undefined)} /> : null}
    </SidebarGroup>
  )
}

interface ProjectItemProps {
  project: Project
  active: boolean
  topics: RunningTopic[]
  openThread: string
  onRename: (project: Project) => void
  onDelete: (project: Project) => void
}

// A project's row; hovering it brings up "…" with rename and delete.
function ProjectItem({ project, active, topics, openThread, onRename, onDelete }: ProjectItemProps) {
  const t = useT()
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={active} className="h-7 text-[0.8125rem]">
        <Link to={project.main_room_id ? `/rooms/${project.main_room_id}` : '/'} aria-current={active ? 'page' : undefined}>
          <ProjectMark name={project.name} active={active} />
          <span className="truncate">{project.name}</span>
        </Link>
      </SidebarMenuButton>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuAction
            showOnHover
            aria-label={t('project.menu', { name: project.name })}
            className="text-subtle peer-data-[size=default]/menu-button:top-1"
          >
            <EllipsisIcon />
          </SidebarMenuAction>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" align="start" className="w-40">
          <DropdownMenuItem onSelect={() => onRename(project)}>
            <PencilIcon />
            {t('project.rename')}
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onSelect={() => onDelete(project)}>
            <Trash2Icon />
            {t('common.delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {topics.length > 0 ? (
        <SidebarMenuSub className="mr-0 gap-0 border-l-0 pl-4">
          {topics.map((topic) => (
            <TopicItem
              key={topic.thread_id}
              topic={topic}
              active={topic.thread_id === openThread}
              named={topic.thread_id === project.wiki_thread_id ? 'wiki' : topic.thread_id === project.setup_thread_id ? 'setup' : undefined}
            />
          ))}
        </SidebarMenuSub>
      ) : null}
    </SidebarMenuItem>
  )
}

// A topic being worked on: a breathing dot and what was asked, opening
// the topic in its chat. The project's wiki topic goes by its name.
function TopicItem({ topic, active, named }: { topic: RunningTopic; active: boolean; named?: 'wiki' | 'setup' }) {
  const t = useT()
  const label = named ? t(`${named}Topic.title`) : topic.ask || topic.root_body.trim() || topic.members.join(t('common.listSeparator'))
  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton asChild isActive={active} size="sm" className="h-6 gap-2 text-[0.78125rem] text-muted-foreground">
        <Link to={`/rooms/${topic.room_id}?thread=${topic.thread_id}`} aria-current={active ? 'page' : undefined}>
          <StatusDot tone="run" />
          <span className="truncate">{label}</span>
        </Link>
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
  )
}
