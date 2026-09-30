import { useId, useRef, useState, type DragEvent } from 'react'
import { ChevronDownIcon, FolderIcon, PackageIcon } from 'lucide-react'
import { useAgents } from '@/api/agents'
import { problemText } from '@/api/errorText'
import type { LocalSkill } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import type { MessageKey } from '@/i18n/zh-CN'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { cn } from '@/lib/utils'
import { droppedFiles, pickedFiles, type Picked } from './pickSkills'
import { UpdateFromHere } from './UpdateFromHere'

// The parts of the import (ImportSkillDialog): the skills on this machine
// to tick, a zip or folder to upload, the agents to install for.

// Why the library cannot take a skill of this machine, by the import's code;
// the codes worded here briefly, the others as the import words them.
const problems: Record<string, MessageKey> = {
  skillBadName: 'library.import.problem.skillBadName',
  skillNoDescription: 'library.import.problem.skillNoDescription',
  skillBuiltin: 'library.import.problem.skillBuiltin',
  skillUnreadable: 'library.import.problem.skillUnreadable',
}

// LocalList lists the skills on this machine to tick, those the library
// cannot take greyed with why. One the library's copy came from can be
// taken in again when it changed since, or when that cannot be told.
export function LocalList({
  skills,
  loading,
  failed,
  ticked,
  onTick,
}: {
  skills?: LocalSkill[]
  loading: boolean
  failed?: string
  ticked: string[]
  onTick: (next: string[]) => void
}) {
  const t = useT()
  const id = useId()
  if (loading) {
    return (
      <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-2 py-1">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-2/3" />
      </div>
    )
  }
  if (failed) return <p className="text-[0.8125rem] text-destructive">{failed}</p>
  if (!skills || skills.length === 0) {
    return <p className="flex min-h-48 items-center justify-center text-[0.8125rem] text-subtle">{t('library.import.localEmpty')}</p>
  }
  return (
    <ul aria-label={t('library.import.tab.local')} className="max-h-64 divide-y overflow-y-auto rounded-md border">
      {skills.map((skill) => {
        const box = `${id}-${skill.folder}`
        const updatable = skill.in_library && (skill.origin === 'changed' || skill.origin === 'unknown')
        // Skills of one name in two folders: the library takes one of them.
        const twins = skills.filter((other) => other.name === skill.name && other.folder !== skill.folder)
        const twinTicked = twins.find((other) => ticked.includes(other.folder))
        const why = skill.in_library
          ? t('library.import.inLibrary')
          : skill.problem
            ? skill.problem in problems
              ? t(problems[skill.problem])
              : problemText(skill.problem, skill.problem_params, t('library.import.problem.skillUnreadable'))
            : twinTicked
              ? t('library.import.twinTicked', { where: twinTicked.where })
              : undefined
        return (
          <li key={skill.folder}>
            <Field orientation="horizontal" className="px-3 py-2" data-disabled={why && !updatable ? true : undefined}>
              <Checkbox
                id={box}
                disabled={why !== undefined}
                checked={ticked.includes(skill.folder)}
                onCheckedChange={(on) => onTick(on === true ? [...ticked, skill.folder] : ticked.filter((folder) => folder !== skill.folder))}
              />
              <FieldContent className="min-w-0 gap-0.5">
                <FieldLabel htmlFor={box} className="flex-wrap font-normal">
                  <span className="font-mono text-[0.8125rem]" translate="no">
                    {skill.name}
                  </span>
                  <span className="text-xs text-subtle">{skill.where}</span>
                  {skill.origin === 'changed' ? <Badge variant="secondary">{t('library.import.changed')}</Badge> : null}
                </FieldLabel>
                {why ? (
                  <FieldDescription className="text-xs">{why}</FieldDescription>
                ) : skill.description ? (
                  // Followed by the line naming its twins, it keeps its place
                  // (FieldDescription lifts one that is next to last).
                  <FieldDescription className="line-clamp-1 text-xs nth-last-2:mt-0">{skill.description}</FieldDescription>
                ) : null}
                {!why && twins.length > 0 ? (
                  <FieldDescription className="text-xs">
                    {t('library.import.twins', { where: twins.map((other) => other.where).join(t('common.listSeparator')) })}
                  </FieldDescription>
                ) : null}
              </FieldContent>
              {updatable ? <UpdateFromHere skill={skill} /> : null}
            </Field>
          </li>
        )
      })}
    </ul>
  )
}

// UploadPicker takes a skill's zip or folder, dropped or chosen.
export function UploadPicker({ picked, onPick }: { picked?: Picked; onPick: (next?: Picked) => void }) {
  const t = useT()
  const zipInput = useRef<HTMLInputElement>(null)
  const folderInput = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)

  async function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    setOver(false)
    onPick(await droppedFiles(event.dataTransfer))
  }

  return (
    <div
      onDragOver={(event) => {
        event.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(event) => void onDrop(event)}
      className={cn(
        'flex min-h-48 flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-4 py-6 text-center transition-colors',
        over && 'border-foreground/40 bg-accent',
      )}
    >
      {picked ? (
        <p className="flex items-center gap-2 text-[0.8125rem]">
          {'zip' in picked.body ? (
            <PackageIcon className="size-4 text-subtle" aria-hidden="true" />
          ) : (
            <FolderIcon className="size-4 text-subtle" aria-hidden="true" />
          )}
          <span className="font-mono" translate="no">
            {picked.name}
          </span>
          <span className="text-xs text-subtle">
            {'zip' in picked.body ? formatBytes(picked.size) : t('library.import.pickedFiles', { n: picked.count, size: formatBytes(picked.size) })}
          </span>
        </p>
      ) : (
        <p className="text-[0.8125rem] text-muted-foreground">{t('library.import.drop')}</p>
      )}
      <div className="flex flex-wrap justify-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => zipInput.current?.click()}>
          {t('library.import.pickZip')}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={() => folderInput.current?.click()}>
          {t('library.import.pickFolder')}
        </Button>
      </div>
      <input
        ref={zipInput}
        type="file"
        accept=".zip,application/zip"
        className="hidden"
        aria-label={t('library.import.pickZip')}
        onChange={(event) => {
          onPick(event.target.files ? pickedFiles(event.target.files) : undefined)
          event.target.value = ''
        }}
      />
      <input
        ref={folderInput}
        type="file"
        multiple
        className="hidden"
        aria-label={t('library.import.pickFolder')}
        {...{ webkitdirectory: '' }}
        onChange={(event) => {
          onPick(event.target.files ? pickedFiles(event.target.files) : undefined)
          event.target.value = ''
        }}
      />
    </div>
  )
}

// AgentPicker picks the agents to install what comes in for, staying open
// to pick several.
export function AgentPicker({ id, value, onChange }: { id: string; value: string[]; onChange: (next: string[]) => void }) {
  const t = useT()
  const agents = useAgents()
  const list = agents.data ?? []
  const names = list.filter((agent) => value.includes(agent.id)).map((agent) => agent.name)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button id={id} type="button" variant="outline" className="w-full justify-between font-normal" disabled={list.length === 0}>
          <span className="truncate">{names.length > 0 ? names.join(t('common.listSeparator')) : t('library.import.noAgents')}</span>
          <ChevronDownIcon className="text-subtle" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-56">
        {list.map((agent) => (
          <DropdownMenuCheckboxItem
            key={agent.id}
            checked={value.includes(agent.id)}
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={(on) => onChange(on === true ? [...value, agent.id] : value.filter((one) => one !== agent.id))}
          >
            <span className="flex min-w-0 flex-col">
              <span className="truncate">{agent.name}</span>
              <span className="truncate text-xs text-muted-foreground">{runtimeName(agent.runtime)}</span>
            </span>
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
