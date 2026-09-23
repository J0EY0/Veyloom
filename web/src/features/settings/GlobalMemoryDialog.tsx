import { useState } from 'react'
import { ChevronLeftIcon, EllipsisIcon, XIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { personalSpace, useWikiHistory } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { formatAgo } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { memoryPage, topicHref } from '@/features/wiki/links'
import { MemoryComposer, MemoryEntries, MemoryHistory, useMemoryEdit } from '@/features/wiki/Memory'

export interface GlobalMemoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

// GlobalMemoryDialog keeps the global memory, the one every project shares
// (docs/design.md 5.19), after ChatGPT's memory: its name, how many entries
// and when it last changed, a menu for its changes, the entries, and a
// line at the foot to add one. A change a member made came from a topic of
// some chat, which opens in its room.
export function GlobalMemoryDialog({ open, onOpenChange }: GlobalMemoryDialogProps) {
  const t = useT()
  const navigate = useNavigate()
  const edit = useMemoryEdit(personalSpace)
  const [view, setView] = useState<'entries' | 'history'>('entries')
  const latest = useWikiHistory(personalSpace, memoryPage, 1, open)
  const memory = edit.memory.data
  const changedAt = latest.data?.[0]?.at
  const facts = [memory ? t('memory.count', { n: memory.entries.length }) : '', changedAt ? t('memory.updated', { ago: formatAgo(changedAt) }) : '']
    .filter(Boolean)
    .join(' · ')

  async function copyFile(file: string) {
    if (await copyText(file)) toast.success(t('wiki.pathCopied'))
    else toast.error(t('common.copyFailed'))
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setView('entries')
        onOpenChange(next)
      }}
    >
      <DialogContent showCloseButton={false} aria-describedby={undefined} className="flex max-h-[min(42rem,85vh)] flex-col gap-0 p-0 sm:max-w-2xl">
        <DialogHeader className="flex-row items-center gap-2 space-y-0 border-b py-2.5 pr-3 pl-5 text-left">
          {view === 'history' ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label={t('memory.entries')} onClick={() => setView('entries')} className="-ml-2 text-subtle">
                  <ChevronLeftIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('memory.entries')}</TooltipContent>
            </Tooltip>
          ) : null}
          {/* The dialog keeps its name; the view says which part is shown. */}
          <DialogTitle className="text-base">{t('memory.global')}</DialogTitle>
          {view === 'history' ? (
            <span className="text-xs text-subtle">{t('wiki.page.history')}</span>
          ) : facts ? (
            <span className="text-xs text-subtle tabular-nums">{facts}</span>
          ) : null}
          <span className="grow" />
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label={t('common.more')} className="text-subtle">
                    <EllipsisIcon />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('common.more')}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => setView(view === 'history' ? 'entries' : 'history')}>
                {view === 'history' ? t('memory.entries') : t('wiki.page.history')}
              </DropdownMenuItem>
              {memory?.file ? <DropdownMenuItem onSelect={() => void copyFile(memory.file as string)}>{t('wiki.page.copyFile')}</DropdownMenuItem> : null}
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DialogClose asChild>
                <Button variant="ghost" size="icon-sm" aria-label={t('common.close')} className="text-subtle">
                  <XIcon />
                </Button>
              </DialogClose>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t('common.close')}</TooltipContent>
          </Tooltip>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
          {view === 'history' ? (
            <div className="px-3">
              <MemoryHistory
                space={personalSpace}
                enabled={open}
                onOpenThread={(threadId, roomId) => {
                  if (roomId) void navigate(topicHref(roomId, threadId))
                }}
              />
            </div>
          ) : (
            <MemoryEntries edit={edit} />
          )}
        </div>
        {view === 'entries' ? (
          <div className="border-t px-4 py-3">
            <MemoryComposer edit={edit} />
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
