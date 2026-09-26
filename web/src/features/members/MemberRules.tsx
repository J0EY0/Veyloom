import { SquareTerminalIcon, XIcon } from 'lucide-react'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useDeleteMemberRule, useMemberRules } from '@/api/rules'
import type { MemberRule } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { commandPattern, ruleText } from '@/features/approvals/describe'
import { formatDay } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { cn } from '@/lib/utils'

export interface MemberRulesProps {
  memberId: string
  // The member's runtime now: a rule kept for another one says whose it is.
  runtime?: string
}

// MemberRules lists what people allowed a member always, each with the
// button that takes it back (docs/design.md 4.6). A rule is added by
// allowing a request "always"; taking one back counts from the member's
// next turn.
export function MemberRules({ memberId, runtime }: MemberRulesProps) {
  const t = useT()
  const rules = useMemberRules(memberId)
  const remove = useDeleteMemberRule(memberId)
  const list = rules.data ?? []

  function revoke(rule: MemberRule, text: string) {
    remove.mutate(rule, {
      onSuccess: () => toast.success(t('member.ruleRevoked', { rule: text })),
      onError: (err) => toast.error(errorText(err)),
    })
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline gap-2 text-sm font-medium">
        <span className="grow">{t('member.rules')}</span>
        {list.length > 0 ? <span className="text-xs font-normal text-subtle tabular-nums">{list.length}</span> : null}
      </div>
      {list.length === 0 ? (
        <p className="text-xs text-subtle">{rules.isPending ? t('common.loading') : t('member.rulesNone')}</p>
      ) : (
        <ul aria-label={t('member.rules')} className="rounded-lg border">
          {list.map((rule, index) => {
            const pattern = commandPattern(rule.rule)
            const text = pattern ?? ruleText(rule.rule)
            const label = t('member.ruleRevoke', { rule: text })
            return (
              <li key={rule.id} className={cn('flex h-9 items-center gap-2.5 pr-1 pl-3', index > 0 && 'border-t')}>
                <SquareTerminalIcon aria-hidden="true" className="size-3.5 flex-none text-subtle" />
                {pattern ? (
                  <code className="min-w-0 grow truncate font-mono text-[0.78125rem] text-foreground" translate="no">
                    {pattern}
                  </code>
                ) : (
                  <span className="min-w-0 grow truncate text-[0.8125rem]">{text}</span>
                )}
                {runtime && rule.runtime !== runtime ? <span className="flex-none text-xs text-subtle">{runtimeName(rule.runtime)}</span> : null}
                <span className="flex-none text-xs text-subtle tabular-nums">{formatDay(rule.created_at)}</span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={label}
                      disabled={remove.isPending}
                      onClick={() => revoke(rule, text)}
                      className="text-subtle"
                    >
                      <XIcon />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{label}</TooltipContent>
                </Tooltip>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
