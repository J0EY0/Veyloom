import { useEffect, useState } from 'react'
import { BotIcon, ChartColumnIcon, InboxIcon, ServerIcon, SettingsIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useProjects } from '@/api/projects'
import { ProjectMark } from '@/components/shared/project-mark'
import { PaletteAttachments, usePaletteAttachments } from '@/features/attachments/PaletteAttachments'
import { openViewer } from '@/features/attachments/viewerStore'
import { Command, CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useT } from '@/lib/i18n'
import { setPaletteOpen, usePaletteOpen } from '@/lib/palette'

// ⌘K: jump to any project's chat or page by typing its name, or, in a
// project's chat, open one of its attachments (docs/webui.md 4.21).
export function CommandPalette() {
  const open = usePaletteOpen()
  const navigate = useNavigate()
  const projects = useProjects()
  const t = useT()
  const [search, setSearch] = useState('')
  const hits = usePaletteAttachments(open ? search : '')
  // The item Enter would pick. Attachments arrive after cmdk has picked
  // the first of what it filtered; with nothing else there, they get it.
  const [selected, setSelected] = useState<string | undefined>()
  const firstHit = hits.attachments[0]?.id
  const [pickedFor, setPickedFor] = useState(firstHit)
  if (pickedFor !== firstHit) {
    setPickedFor(firstHit)
    if (firstHit !== undefined && !selected) setSelected(`attachment ${firstHit}`)
  }

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'k' && (event.metaKey || event.ctrlKey)) {
        event.preventDefault()
        setPaletteOpen(true)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])

  function close() {
    setPaletteOpen(false)
    setSearch('')
    setSelected(undefined)
  }

  function go(to: string) {
    close()
    void navigate(to)
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => (next ? setPaletteOpen(true) : close())}
      title={t('palette.title')}
      description={t('palette.placeholder')}
    >
      <Command label={t('palette.title')} value={selected ?? ''} onValueChange={setSelected}>
        <CommandInput placeholder={t('palette.placeholder')} value={search} onValueChange={setSearch} />
        <CommandList>
          {/* cmdk counts only what it filtered itself, not the attachments the hub found. */}
          {hits.attachments.length === 0 ? <CommandEmpty>{t('palette.empty')}</CommandEmpty> : null}
          <CommandGroup heading={t('palette.projects')}>
            {(projects.data ?? []).map((project) => (
              <CommandItem key={project.id} value={project.name} onSelect={() => go(`/rooms/${project.main_room_id}`)}>
                <ProjectMark name={project.name} />
                <span className="truncate">{project.name}</span>
              </CommandItem>
            ))}
          </CommandGroup>
          <CommandGroup heading={t('palette.pages')}>
            <CommandItem value={t('nav.inbox')} onSelect={() => go('/inbox')}>
              <InboxIcon className="text-subtle" />
              {t('nav.inbox')}
            </CommandItem>
            <CommandItem value={t('nav.agents')} onSelect={() => go('/agents')}>
              <BotIcon className="text-subtle" />
              {t('nav.agents')}
            </CommandItem>
            <CommandItem value={t('nav.machines')} onSelect={() => go('/machines')}>
              <ServerIcon className="text-subtle" />
              {t('nav.machines')}
            </CommandItem>
            <CommandItem value={t('nav.usage')} onSelect={() => go('/usage')}>
              <ChartColumnIcon className="text-subtle" />
              {t('nav.usage')}
            </CommandItem>
            <CommandItem value={t('nav.settings')} onSelect={() => go('/settings')}>
              <SettingsIcon className="text-subtle" />
              {t('nav.settings')}
            </CommandItem>
          </CommandGroup>
          <PaletteAttachments
            hits={hits}
            onOpen={(attachment) => {
              close()
              openViewer({ roomId: hits.roomId, attachment, threadId: attachment.thread_id })
            }}
            onSeeAll={() => go(`/rooms/${hits.roomId}/attachments?q=${encodeURIComponent(hits.q)}`)}
          />
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
