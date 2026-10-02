import { useId, useState } from 'react'
import { ArrowUpIcon, ChevronRightIcon, PencilIcon, Trash2Icon } from 'lucide-react'
import { errorText } from '@/api/errorText'
import type { MemoryEntry } from '@/api/types'
import { useMemory, useSaveMemory, useWikiHistory, type MemorySpace } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Item, ItemActions, ItemContent, ItemDescription, ItemGroup, ItemSeparator } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatDay } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { CommitRow, type OpenTopic } from './CommitRow'
import { memoryPage } from './links'

// A memory every turn carries whole (docs/design.md 5.16), kept by hand:
// its entries, each with the day it was noted and who noted it; a way to
// add one, change one and take one out, each saved at once as a change
// that can be undone; and its changes. The project memory's page and the
// global memory's dialog in Settings (5.19) put these parts together.

// How full a memory is before the person is told to fold it.
const nearlyFull = 0.9

// useMemoryEdit is a memory being kept: what it holds, and the entry being
// added or changed. Every change is saved from what was read.
export function useMemoryEdit(space: MemorySpace) {
  const memory = useMemory(space)
  const save = useSaveMemory(space)
  const [draft, setDraft] = useState('')
  const [editing, setEditing] = useState<{ index: number; text: string } | null>(null)
  const view = memory.data
  const texts = view?.entries.map((entry) => entry.text) ?? []
  const commit = (entries: string[], done: () => void) => {
    if (view) save.mutate({ entries, hash: view.hash }, { onSuccess: done })
  }
  return {
    memory,
    save,
    draft,
    setDraft,
    editing,
    setEditing,
    full: view ? view.chars >= view.budget * nearlyFull : false,
    add: () => {
      if (draft.trim() !== '') commit([...texts, draft], () => setDraft(''))
    },
    startEditing: (index: number, text: string) => {
      save.reset()
      setEditing({ index, text })
    },
    change: () => {
      if (editing)
        commit(
          texts.map((text, index) => (index === editing.index ? editing.text : text)),
          () => setEditing(null),
        )
    },
    remove: (index: number) =>
      commit(
        texts.filter((_, at) => at !== index),
        () => setEditing(null),
      ),
  }
}

export type MemoryEdit = ReturnType<typeof useMemoryEdit>

// MemoryEntries lists the entries, one changed in place when asked.
export function MemoryEntries({ edit, className }: { edit: MemoryEdit; className?: string }) {
  const t = useT()
  const { memory, save, editing } = edit
  if (memory.isPending) {
    return (
      <div role="status" aria-label={t('common.loading')} className={cn('flex flex-col gap-2', className)}>
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    )
  }
  if (memory.isError) {
    return (
      <p role="alert" className={cn('text-[0.8125rem] text-status-fail', className)}>
        {errorText(memory.error)}
      </p>
    )
  }
  const entries = memory.data.entries
  // Nothing kept yet: one line in the middle, not an empty frame.
  if (entries.length === 0) {
    return (
      <Empty className="gap-0 py-10 md:py-10">
        <EmptyHeader>
          <EmptyTitle className="text-[0.8125rem] font-normal tracking-normal text-muted-foreground">{t('memory.empty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <ItemGroup aria-label={t('memory.entries')} className={className}>
      {entries.map((entry, index) => (
        <div key={`${index}:${entry.text}`}>
          {index > 0 ? <ItemSeparator /> : null}
          {editing?.index === index ? (
            <EntryEditor
              text={editing.text}
              busy={save.isPending}
              onChange={(text) => edit.setEditing({ index, text })}
              onSave={edit.change}
              onCancel={() => edit.setEditing(null)}
            />
          ) : (
            <EntryRow entry={entry} busy={save.isPending} onEdit={() => edit.startEditing(index, entry.text)} onRemove={() => edit.remove(index)} />
          )}
        </div>
      ))}
    </ItemGroup>
  )
}

// MemoryComposer adds an entry: a line and a button to send it, then what
// went wrong saving, and how full the memory is once that matters.
export function MemoryComposer({ edit }: { edit: MemoryEdit }) {
  const t = useT()
  const id = useId()
  const { draft, setDraft, save, editing } = edit
  const view = edit.memory.data
  return (
    <div className="flex flex-col gap-2">
      <form
        onSubmit={(event) => {
          event.preventDefault()
          edit.add()
        }}
      >
        <InputGroup className="h-10 rounded-full bg-background pl-1.5">
          <InputGroupInput
            id={id}
            aria-label={t('memory.addLabel')}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={t('memory.addPlaceholder')}
            autoComplete="off"
            className="text-[0.8125rem]"
          />
          <InputGroupAddon align="inline-end">
            <Tooltip>
              <TooltipTrigger asChild>
                <InputGroupButton
                  type="submit"
                  variant="default"
                  size="icon-xs"
                  aria-label={t('memory.add')}
                  disabled={!view || save.isPending || draft.trim() === ''}
                  className="rounded-full"
                >
                  {save.isPending && !editing ? <Spinner /> : <ArrowUpIcon />}
                </InputGroupButton>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('memory.add')}</TooltipContent>
            </Tooltip>
          </InputGroupAddon>
        </InputGroup>
      </form>
      {save.error ? (
        <p role="alert" className="text-[0.8125rem] leading-relaxed text-status-fail">
          {errorText(save.error)}
        </p>
      ) : null}
      {view && edit.full ? <p className="text-xs text-status-wait tabular-nums">{t('memory.usage', { chars: view.chars, budget: view.budget })}</p> : null}
    </div>
  )
}

// MemoryEditor is a memory kept on a page: its entries, a line to add
// one, and its changes, folded until asked for.
export function MemoryEditor({ space, onOpenThread }: { space: MemorySpace; onOpenThread: OpenTopic }) {
  const t = useT()
  const edit = useMemoryEdit(space)
  const [open, setOpen] = useState(false)
  return (
    <div className="flex flex-col gap-3">
      <MemoryEntries edit={edit} className="overflow-hidden rounded-xl border bg-card" />
      <MemoryComposer edit={edit} />
      <Collapsible open={open} onOpenChange={setOpen} className="group/history mt-5">
        <CollapsibleTrigger className="flex items-center gap-1 text-xs font-medium text-subtle hover:text-foreground">
          <ChevronRightIcon className="size-3.5 transition-transform group-data-[state=open]/history:rotate-90" />
          {t('wiki.page.history')}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <MemoryHistory space={space} onOpenThread={onOpenThread} enabled={open} />
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}

// MemoryHistory is the memory's changes, read once enabled; any of them
// can be undone.
export function MemoryHistory({ space, onOpenThread, enabled }: { space: MemorySpace; onOpenThread: OpenTopic; enabled: boolean }) {
  const t = useT()
  const history = useWikiHistory(space, memoryPage, 30, enabled)
  if (history.isPending) {
    return (
      <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
        <Spinner className="size-3" />
        {t('common.loading')}
      </p>
    )
  }
  if ((history.data ?? []).length === 0) return <p className="py-2 text-xs text-subtle">{t('memory.noHistory')}</p>
  return (
    <ul className="divide-y">
      {(history.data ?? []).map((commit) => (
        <CommitRow key={commit.sha} commit={commit} space={space} canUndo onOpenThread={onOpenThread} />
      ))}
    </ul>
  )
}

// entryMeta is when an entry was noted and by whom, in the UI's words: a
// member's note names the topic it came from, and in the global memory
// the project ("Claude in topic #4 of Veyloom", as the page has it).
function entryMeta(t: ReturnType<typeof useT>, entry: MemoryEntry): string {
  let source = entry.source ?? ''
  const noted = /^(.+) in topic #(\d+)(?: of (.+))?$/.exec(source)
  if (noted) {
    source = noted[3]
      ? t('memory.source.projectTopic', { who: noted[1], n: noted[2], project: noted[3] })
      : t('memory.source.topic', { who: noted[1], n: noted[2] })
  }
  return [entry.date ? formatDay(entry.date) : '', source].filter((part) => part !== '').join(' · ')
}

function EntryRow({ entry, busy, onEdit, onRemove }: { entry: MemoryEntry; busy: boolean; onEdit: () => void; onRemove: () => void }) {
  const t = useT()
  const meta = entryMeta(t, entry)
  return (
    <Item role="listitem" size="sm" className="items-start gap-3 py-2.5">
      <ItemContent className="min-w-0 gap-0.5">
        <p className="text-[0.8125rem] leading-relaxed break-words">{entry.text}</p>
        {meta ? <ItemDescription className="text-xs text-subtle">{meta}</ItemDescription> : null}
      </ItemContent>
      <ItemActions className="-my-0.5 gap-0.5">
        <IconAction label={t('memory.edit')} disabled={busy} onClick={onEdit}>
          <PencilIcon />
        </IconAction>
        <IconAction label={t('memory.delete')} disabled={busy} onClick={onRemove}>
          <Trash2Icon />
        </IconAction>
      </ItemActions>
    </Item>
  )
}

function IconAction({ label, disabled, onClick, children }: { label: string; disabled: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={label} disabled={disabled} onClick={onClick} className="text-subtle hover:text-foreground">
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  )
}

function EntryEditor({
  text,
  busy,
  onChange,
  onSave,
  onCancel,
}: {
  text: string
  busy: boolean
  onChange: (text: string) => void
  onSave: () => void
  onCancel: () => void
}) {
  const t = useT()
  return (
    <form
      className="flex flex-col gap-2 px-4 py-2.5"
      onSubmit={(event) => {
        event.preventDefault()
        onSave()
      }}
    >
      <Textarea
        aria-label={t('memory.edit')}
        value={text}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          // One line an entry: Enter saves it, Escape leaves it as it was.
          if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
            event.preventDefault()
            onSave()
          } else if (event.key === 'Escape') {
            event.preventDefault()
            onCancel()
          }
        }}
        autoFocus
        // Opened to be changed, it starts with the caret at the end.
        onFocus={(event) => event.currentTarget.setSelectionRange(event.currentTarget.value.length, event.currentTarget.value.length)}
        rows={2}
        className="min-h-0 text-[0.8125rem]"
      />
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" size="sm" disabled={busy || text.trim() === ''}>
          {busy ? <Spinner data-icon="inline-start" /> : null}
          {t('memory.save')}
        </Button>
      </div>
    </form>
  )
}
