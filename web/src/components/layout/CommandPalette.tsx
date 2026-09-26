import { useEffect } from 'react'
import { BotIcon, ChartColumnIcon, InboxIcon, ServerIcon, SettingsIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useProjects } from '@/api/projects'
import { ProjectMark } from '@/components/shared/project-mark'
import { Command, CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useT } from '@/lib/i18n'
import { setPaletteOpen, usePaletteOpen } from '@/lib/palette'

// ⌘K: jump to any project's chat or page by typing its name.
export function CommandPalette() {
  const open = usePaletteOpen()
  const navigate = useNavigate()
  const projects = useProjects()
  const t = useT()

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

  function go(to: string) {
    setPaletteOpen(false)
    void navigate(to)
  }

  return (
    <CommandDialog open={open} onOpenChange={setPaletteOpen} title={t('palette.title')} description={t('palette.placeholder')}>
      <Command label={t('palette.title')}>
        <CommandInput placeholder={t('palette.placeholder')} />
        <CommandList>
          <CommandEmpty>{t('palette.empty')}</CommandEmpty>
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
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
