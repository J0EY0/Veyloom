import { CircleAlertIcon, InfoIcon, TriangleAlertIcon } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { Notice, NoticeLevel } from './activity'

const looks = {
  info: { Icon: InfoIcon, tone: 'text-muted-foreground', key: 'notice.info' },
  warning: { Icon: TriangleAlertIcon, tone: 'text-status-wait', key: 'notice.warning' },
  error: { Icon: CircleAlertIcon, tone: 'text-status-fail', key: 'notice.error' },
} as const satisfies Record<NoticeLevel, unknown>

export interface NoticeListProps {
  notices: Notice[]
  className?: string
}

// What a runtime told people during a turn, shown as it is rather than
// folded into the activity line: its warnings, the errors it carried on
// after, the requests Veyloom could not answer (docs/design.md 4.6).
export function NoticeList({ notices, className }: NoticeListProps) {
  const t = useT()
  if (notices.length === 0) return null
  return (
    <div className={cn('my-1 flex flex-col gap-1', className)}>
      {notices.map((notice, index) => {
        const { Icon, tone, key } = looks[notice.level]
        return (
          <Alert
            key={index}
            role="note"
            className="rounded-md border-0 bg-muted px-2.5 py-1.5 has-[>svg]:gap-x-2 [&>svg]:size-3.5 [&>svg]:translate-y-[0.15625rem]"
          >
            <Icon className={tone} aria-hidden />
            <AlertDescription className="text-[0.78125rem] leading-normal break-words text-muted-foreground">
              <span className="sr-only">{t(key)}: </span>
              {notice.text}
            </AlertDescription>
          </Alert>
        )
      })}
    </div>
  )
}
