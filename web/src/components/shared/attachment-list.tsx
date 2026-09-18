import { PaperclipIcon } from 'lucide-react'
import { attachmentUrl } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { MessageAttachment, MessageAttachments } from '@/components/ai-elements/message'
import { Button } from '@/components/ui/button'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export interface AttachmentListProps {
  attachments: Attachment[] | null | undefined
  className?: string
}

// The files under a message, drawn with AI Elements' attachment tiles: an
// image is a thumbnail that opens full size, anything else a chip that
// downloads.
export function AttachmentList({ attachments, className }: AttachmentListProps) {
  const t = useT()
  if (!attachments?.length) return null
  return (
    <MessageAttachments aria-label={t('attachment.list')} className={cn('mt-2 ml-0 items-center', className)}>
      {attachments.map((attachment) => {
        const url = attachmentUrl(attachment.id)
        if (attachment.media_type.startsWith('image/')) {
          return (
            <a
              key={attachment.id}
              href={url}
              target="_blank"
              rel="noreferrer"
              aria-label={t('attachment.open', { name: attachment.filename })}
              className="rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            >
              <MessageAttachment
                data={{ type: 'file', url, mediaType: attachment.media_type, filename: attachment.filename }}
                className="border bg-muted transition-opacity hover:opacity-90"
              />
            </a>
          )
        }
        return (
          <Button key={attachment.id} asChild variant="outline" size="sm" className="max-w-64 justify-start bg-muted font-normal">
            <a href={url} download={attachment.filename} aria-label={t('attachment.open', { name: attachment.filename })}>
              <PaperclipIcon className="text-subtle" />
              <span className="min-w-0 truncate">{attachment.filename}</span>
              <span className="flex-none text-xs text-subtle tabular-nums">{formatBytes(attachment.size)}</span>
            </a>
          </Button>
        )
      })}
    </MessageAttachments>
  )
}
