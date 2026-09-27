import { useState } from 'react'
import { ChevronDownIcon, Maximize2Icon } from 'lucide-react'
import { useAttachmentText } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { CodeBlock } from '@/components/ai-elements/code-block'
import { MessageResponse } from '@/components/ai-elements/message'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { DownloadButton, FileBadge } from './FileCard'
import { firstLines, isMarkdown, languageOf, lineCount, withoutLastBreak } from './kinds'

// How much of a text file the chat reads, and how many of its lines it
// shows before "show the rest".
export const TEXT_HEAD = 32 * 1024
export const PREVIEW_LINES = 9

// TextPreview is a text file where it was sent (docs/webui.md 4.21): its
// first lines, code highlighted and Markdown drawn, the rest a click away.
export function TextPreview({ attachment, onOpen, className }: { attachment: Attachment; onOpen: () => void; className?: string }) {
  const t = useT()
  const head = useAttachmentText(attachment.id, TEXT_HEAD)
  const [open, setOpen] = useState(false)
  const text = head.data?.text ?? ''
  const lines = lineCount(text)
  const rest = lines - PREVIEW_LINES
  const shown = withoutLastBreak(open ? text : firstLines(text, PREVIEW_LINES))
  const markdown = isMarkdown(attachment.filename)

  return (
    <div className={cn('w-155 max-w-full overflow-hidden rounded-xl border bg-card', className)}>
      <div className="flex h-10.5 items-center gap-2.5 border-b pr-1.5 pl-3">
        <FileBadge attachment={attachment} className="size-6 text-[0.5625rem]" />
        <span className="min-w-0 truncate text-[0.8125rem] font-medium text-foreground">{attachment.filename}</span>
        <span className="flex-none text-xs text-subtle tabular-nums">
          {head.data && !head.data.more ? `${t('attachment.lines', { n: lines })} · ` : ''}
          {formatBytes(attachment.size)}
        </span>
        <span className="grow" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onOpen}
              aria-label={t('attachment.preview', { name: attachment.filename })}
              className="text-subtle hover:text-foreground"
            >
              <Maximize2Icon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('attachment.previewShort')}</TooltipContent>
        </Tooltip>
        <DownloadButton attachment={attachment} />
      </div>
      {head.isPending ? (
        <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-2 p-4">
          <Skeleton className="h-3 w-2/3" />
          <Skeleton className="h-3 w-1/2" />
          <Skeleton className="h-3 w-3/5" />
        </div>
      ) : head.isError ? (
        <p className="px-4 py-3 text-xs text-subtle">{t('attachment.readFailed')}</p>
      ) : markdown ? (
        <MessageResponse className="prose-agent px-4 py-3 text-[0.8125rem]">{shown}</MessageResponse>
      ) : (
        <CodeBlock code={shown} language={languageOf(attachment.filename)} showLineNumbers className="rounded-none border-0 [&_pre]:text-[0.78125rem]!" />
      )}
      {rest > 0 || (open && head.data?.more) ? (
        <div className="flex h-8.5 items-center justify-center border-t">
          {open && head.data?.more ? (
            <Button variant="ghost" size="sm" onClick={onOpen} className="h-7 text-xs font-normal text-muted-foreground">
              {t('attachment.wholeFile')}
            </Button>
          ) : (
            <Button variant="ghost" size="sm" aria-expanded={open} onClick={() => setOpen((v) => !v)} className="h-7 text-xs font-normal text-muted-foreground">
              {open ? t('attachment.collapse') : head.data?.more ? t('attachment.expandMore') : t('attachment.expand', { n: rest })}
              <ChevronDownIcon className={cn('transition-transform', open && 'rotate-180')} />
            </Button>
          )}
        </div>
      ) : null}
    </div>
  )
}
