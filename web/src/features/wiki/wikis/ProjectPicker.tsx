import { useMemo, useState } from 'react'
import { CheckIcon, ChevronsUpDownIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import type { Project } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandSeparator } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { wikisHref } from '../links'
import { rememberWiki } from './lastWiki'

export interface ProjectPickerProps {
  projects: Project[]
  // The project whose wiki is shown; none for every project's.
  current: string
}

// ProjectPicker switches the Wiki page between every project's wiki and
// one project's (docs/design.md 5.18): shadcn's combobox, a list to find a
// project in by name.
export function ProjectPicker({ projects, current }: ProjectPickerProps) {
  const t = useT()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const sorted = useMemo(() => [...projects].sort((a, b) => a.name.localeCompare(b.name)), [projects])
  // A project's name, once the list has it; never "all" for one.
  const name = current ? (projects.find((project) => project.id === current)?.name ?? '') : t('wikis.all')
  const pick = (projectId: string) => {
    setOpen(false)
    rememberWiki(projectId)
    void navigate(wikisHref(projectId))
  }
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" role="combobox" aria-expanded={open} className="min-w-0 gap-1 px-2 font-normal text-muted-foreground">
          <span className="truncate">{name}</span>
          <ChevronsUpDownIcon className="size-3.5 text-subtle" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(18rem,calc(100vw-2rem))] p-0">
        <Command>
          <CommandInput placeholder={t('wikis.find')} />
          <CommandList>
            <CommandEmpty>{t('wikis.noProject')}</CommandEmpty>
            <CommandGroup>
              <CommandItem value="all projects" keywords={[t('wikis.all')]} onSelect={() => pick('')}>
                {t('wikis.all')}
                <CheckIcon className={cn('ml-auto', current !== '' && 'invisible')} />
              </CommandItem>
            </CommandGroup>
            {sorted.length > 0 ? <CommandSeparator /> : null}
            <CommandGroup>
              {sorted.map((project) => (
                <CommandItem key={project.id} value={project.id} keywords={[project.name]} onSelect={() => pick(project.id)}>
                  <span className="truncate">{project.name}</span>
                  <CheckIcon className={cn('ml-auto', project.id !== current && 'invisible')} />
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
