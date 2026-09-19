import { ExternalLinkIcon } from 'lucide-react'
import { useState } from 'react'
import { ApiError } from '@/api/client'
import { useDecideApproval } from '@/api/approvals'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import type { MessageKey } from '@/i18n/zh-CN'
import { useCurrentUser } from '@/lib/currentUser'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { ApprovalCardProps } from './ApprovalCard'
import { isWebLink, linkOf } from './forms'
import { decisionNote } from './describe'

const heads = {
  pending: { tone: 'wait', key: 'link.asking' },
  allowed: { tone: 'ok', key: 'link.done' },
  denied: { tone: 'idle', key: 'link.declined' },
  expired: { tone: 'idle', key: 'link.expired' },
  cancelled: { tone: 'idle', key: 'link.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

// A page an MCP server asks a person to open, such as a sign-in (docs/
// design.md 4.6). The link is shown in full, its host set apart, and only
// opened by the person's own click; they say when they are done, or
// decline. Anything but http or https is shown and never opened.
export function LinkCard({ approval, memberName, names }: ApprovalCardProps) {
  const t = useT()
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const [error, setError] = useState<string>()
  const link = linkOf(approval) ?? { server: '', message: '', url: '' }
  const safe = isWebLink(link.url)
  const host = safe ? new URL(link.url).host : ''
  const pending = approval.status === 'pending'
  const head = heads[approval.status as keyof typeof heads] ?? heads.cancelled
  const who = approval.decided_by ? (names.get(approval.decided_by) ?? t('approval.someone')) : undefined

  function send(allow: boolean) {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, message: '' },
      { onError: (err) => setError(err instanceof ApiError && err.status === 409 ? t('approval.raced') : err.message) },
    )
  }

  return (
    <div
      role="group"
      aria-label={t('link.label')}
      className={cn(
        'mt-2.5 flex flex-col gap-2.5 rounded-[10px] bg-muted px-3.5 py-3 text-foreground',
        (approval.status === 'expired' || approval.status === 'cancelled') && 'opacity-55',
      )}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone={head.tone} />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        <span className={cn(pending && 'font-medium text-foreground')}>{t(head.key)}</span>
      </div>
      <div className="grid gap-0.5">
        {link.message ? <p className="text-[0.8125rem] break-words whitespace-pre-wrap">{link.message}</p> : null}
        {link.server ? <p className="text-[0.75rem] text-subtle">{t('form.from', { server: link.server })}</p> : null}
      </div>
      <p className="rounded-md bg-background px-2.5 py-1.5 font-mono text-[0.75rem] break-all text-muted-foreground" translate="no">
        {host ? <b className="font-medium text-foreground">{host}</b> : null}
        {host ? <br /> : null}
        {link.url}
      </p>
      {safe ? null : (
        <p role="alert" className="text-xs text-status-fail">
          {t('link.unsafe')}
        </p>
      )}
      {pending ? (
        <div className="flex items-center gap-1.5">
          <span className="text-xs text-subtle">
            {error ? (
              <span role="alert" className="text-status-fail">
                {error}
              </span>
            ) : user ? null : (
              t('common.identifyingShort')
            )}
          </span>
          <span className="grow" />
          {safe ? (
            <Button asChild variant="outline" size="sm">
              <a href={link.url} target="_blank" rel="noopener noreferrer">
                <ExternalLinkIcon aria-hidden="true" />
                {t('link.open')}
              </a>
            </Button>
          ) : null}
          <Button variant="ghost" size="sm" onClick={() => send(false)} disabled={!user || decide.isPending}>
            {t('link.decline')}
          </Button>
          <Button size="sm" onClick={() => send(true)} disabled={!user || decide.isPending} aria-busy={decide.isPending}>
            {t('link.finish')}
          </Button>
        </div>
      ) : (
        <div className="text-[0.78125rem] text-subtle">
          {who === undefined ? t('form.nobody') : approval.status === 'allowed' ? t('link.doneBy', { who }) : t('link.declinedBy', { who })}
          {approval.decided_at ? ` · ${formatTime(approval.decided_at)}` : ''}
          {approval.message ? ` · “${decisionNote(approval.message)}”` : ''}
        </div>
      )}
    </div>
  )
}
