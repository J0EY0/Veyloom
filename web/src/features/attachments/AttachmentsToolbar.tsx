import { useEffect, useState } from 'react'
import { ArrowDownUpIcon, ChevronDownIcon, SearchIcon, SquareCheckIcon } from 'lucide-react'
import type { AttachmentSort } from '@/api/types'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { formatCount } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { sorts, type TabFilter } from './filter'
import type { KindGroup } from './kinds'

export interface AttachmentsToolbarProps {
  roomId: string
  filter: TabFilter
  onChange: (change: Partial<TabFilter>) => void
  // How many the filter finds, when the list is not counted by days.
  total?: number
  picking: boolean
  onPicking: (picking: boolean) => void
  // Every card shown is picked.
  allPicked: boolean
  onPickAll: (all: boolean) => void
}

// How long typing rests before the words are looked for.
const TYPING_MS = 250

// The quiet row over the attachments tab's cards (docs/webui.md 4.21): a
// search box without a frame, the kinds, then buttons without frames for
// the sender, the order and picking.
export function AttachmentsToolbar({ roomId, filter, onChange, total, picking, onPicking, allPicked, onPickAll }: AttachmentsToolbarProps) {
  const t = useT()
  const { all } = useMentionTargets(roomId)
  const [words, setWords] = useState(filter.q)
  // The address can change the words too: the search's "see them all".
  // Words it has already, being typed with a space after them, stay.
  const [shownQ, setShownQ] = useState(filter.q)
  if (shownQ !== filter.q) {
    setShownQ(filter.q)
    if (filter.q !== words.trim()) setWords(filter.q)
  }
  useEffect(() => {
    if (words.trim() === filter.q.trim()) return
    const timer = setTimeout(() => onChange({ q: words }), TYPING_MS)
    return () => clearTimeout(timer)
  }, [words, filter.q, onChange])

  const senders = all.map((target) => ({ value: `${target.mention.kind === 'agent' ? 'member' : 'user'}:${target.mention.id}`, name: target.name }))
  const sender = senders.find((s) => s.value === filter.sender)
  const ghost = 'h-7.5 gap-1.5 px-2.5 text-[0.8125rem] font-normal text-foreground [&_svg]:text-subtle'

  return (
    <div className="flex flex-wrap items-center gap-2">
      <InputGroup className="h-8 w-75 max-w-full border-0 bg-muted/70 shadow-none has-[[data-slot=input-group-control]:focus-visible]:ring-2 dark:bg-input/30">
        <InputGroupAddon align="inline-start">
          <SearchIcon className="size-3.5 text-subtle" />
        </InputGroupAddon>
        <InputGroupInput
          type="search"
          value={words}
          onChange={(event) => setWords(event.target.value)}
          placeholder={t('attachments.search')}
          aria-label={t('attachments.searchLabel')}
          className="text-[0.8125rem]"
        />
      </InputGroup>
      <Tabs value={filter.group} onValueChange={(group) => onChange({ group: group as KindGroup })}>
        <TabsList aria-label={t('attachments.kinds')} className="h-8">
          {(['all', 'media', 'docs', 'other'] as const).map((group) => (
            <TabsTrigger key={group} value={group} className="px-2.5 text-xs">
              {t(`attachments.kind.${group}`)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <span className="grow" />
      {total !== undefined ? <span className="px-1 text-xs text-subtle tabular-nums">{t('attachments.count', { n: formatCount(total) })}</span> : null}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" aria-label={`${t('attachments.sender')}: ${sender?.name ?? t('attachments.everyone')}`} className={ghost}>
            <span className="max-w-32 truncate">{sender?.name ?? t('attachments.everyone')}</span>
            <ChevronDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="max-h-80 w-52">
          <DropdownMenuLabel className="text-xs text-subtle">{t('attachments.sender')}</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={filter.sender} onValueChange={(value) => onChange({ sender: value })}>
            <DropdownMenuRadioItem value="">{t('attachments.everyone')}</DropdownMenuRadioItem>
            <DropdownMenuSeparator />
            {senders.map((s) => (
              <DropdownMenuRadioItem key={s.value} value={s.value}>
                <span className="truncate">{s.name}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" aria-label={`${t('attachments.sort')}: ${t(`attachments.sort.${filter.sort}`)}`} className={ghost}>
            <ArrowDownUpIcon />
            {t(`attachments.sort.${filter.sort}`)}
            <ChevronDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuRadioGroup value={filter.sort} onValueChange={(value) => onChange({ sort: value as AttachmentSort })}>
            {sorts.map((sort) => (
              <DropdownMenuRadioItem key={sort} value={sort}>
                {t(`attachments.sort.${sort}`)}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      {picking ? (
        <Button variant="ghost" size="sm" onClick={() => onPickAll(!allPicked)} className={ghost}>
          {allPicked ? t('attachments.selectNone') : t('attachments.selectAll')}
        </Button>
      ) : null}
      <Button variant="ghost" size="sm" aria-pressed={picking} onClick={() => onPicking(!picking)} className={cn(ghost, picking && 'bg-accent')}>
        <SquareCheckIcon />
        {t('attachments.select')}
      </Button>
    </div>
  )
}
