import { useId, useState } from 'react'
import { useDecideApproval } from '@/api/approvals'
import type { Approval } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import type { MessageKey } from '@/i18n/zh-CN'
import { useCurrentUser } from '@/lib/currentUser'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { ApprovalCardProps } from './ApprovalCard'
import { FormFieldControl } from './FormFieldControl'
import { type FormField, type FormValue, contentOf, formOf, problemOf } from './forms'
import { decisionNote } from './describe'
import { errorText } from '@/api/errorText'

const heads = {
  pending: { tone: 'wait', key: 'form.asking' },
  allowed: { tone: 'ok', key: 'form.sent' },
  denied: { tone: 'idle', key: 'form.declined' },
  expired: { tone: 'idle', key: 'form.expired' },
  cancelled: { tone: 'idle', key: 'form.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

// A form an MCP server asks a person to fill in, through whichever runtime
// (docs/design.md 4.6): its message, where it comes from, and its fields
// as the server described them. Sent back filled in, or declined; once
// settled it shows what was sent.
export function FormCard({ approval, memberName, names }: ApprovalCardProps) {
  const t = useT()
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const idPrefix = useId()
  const [values, setValues] = useState<Record<string, FormValue>>({})
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [error, setError] = useState<string>()
  const form = formOf(approval) ?? { server: '', message: '', fields: [] }
  const pending = approval.status === 'pending'
  const head = heads[approval.status as keyof typeof heads] ?? heads.cancelled
  const valueOf = (field: FormField) => values[field.name] ?? field.initial
  const problems = new Map(form.fields.map((field) => [field.name, problemOf(field, valueOf(field))]))
  const ready = form.fields.every((field) => problems.get(field.name) === undefined)

  function send(allow: boolean) {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, message: '', ...(allow ? { answer: { content: contentOf(form.fields, values) } } : {}) },
      { onError: (err) => setError(errorText(err, { 409: t('approval.raced') })) },
    )
  }

  return (
    <div
      role="group"
      aria-label={t('form.label')}
      className={cn(
        'mt-2.5 flex flex-col gap-3 rounded-[10px] bg-muted px-3.5 py-3 text-foreground',
        (approval.status === 'expired' || approval.status === 'cancelled') && 'opacity-55',
      )}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone={head.tone} />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        <span className={cn(pending && 'font-medium text-foreground')}>{t(head.key)}</span>
      </div>
      <div className="grid gap-0.5">
        {form.message ? <p className="text-[0.8125rem] break-words whitespace-pre-wrap">{form.message}</p> : null}
        {form.server ? <p className="text-[0.75rem] text-subtle">{t('form.from', { server: form.server })}</p> : null}
      </div>
      {pending ? (
        <>
          <FieldGroup className="gap-3.5">
            {form.fields.map((field, index) => (
              <FormFieldControl
                key={field.name}
                field={field}
                id={`${idPrefix}-${index}`}
                value={valueOf(field)}
                onChange={(value) => {
                  setValues((old) => ({ ...old, [field.name]: value }))
                  setTouched((old) => ({ ...old, [field.name]: true }))
                }}
                problem={touched[field.name] ? problems.get(field.name) : undefined}
                disabled={decide.isPending}
              />
            ))}
          </FieldGroup>
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
            <Button variant="ghost" size="sm" onClick={() => send(false)} disabled={!user || decide.isPending}>
              {t('form.decline')}
            </Button>
            <Button size="sm" onClick={() => send(true)} disabled={!user || !ready || decide.isPending} aria-busy={decide.isPending}>
              {t('form.submit')}
            </Button>
          </div>
        </>
      ) : (
        <SentForm approval={approval} fields={form.fields} names={names} />
      )}
    </div>
  )
}

// SentForm shows a form once it is over: what was sent, field by field,
// and who settled it when.
function SentForm({ approval, fields, names }: { approval: Approval; fields: FormField[]; names: Map<string, string> }) {
  const t = useT()
  const content = contentFrom(approval.answer)
  const who = approval.decided_by ? (names.get(approval.decided_by) ?? t('approval.someone')) : undefined
  const line = who === undefined ? t('form.nobody') : approval.status === 'allowed' ? t('form.sentBy', { who }) : t('form.declinedBy', { who })
  const shown = (field: FormField): string => {
    const value = content[field.name]
    if (value === undefined) return '—'
    if (field.type === 'boolean') return value === true ? t('form.yes') : t('form.no')
    const label = (v: unknown) =>
      field.type === 'choice' || field.type === 'choices' ? (field.choices.find((c) => c.value === v)?.label ?? String(v)) : String(v)
    return Array.isArray(value) ? value.map(label).join('、') : label(value)
  }
  return (
    <>
      {approval.status === 'allowed' ? (
        <dl className="grid gap-2 text-[0.8125rem]">
          {fields.map((field) => (
            <div key={field.name} className="grid gap-0.5">
              <dt className="text-muted-foreground">{field.label}</dt>
              <dd className="break-words text-foreground">{shown(field)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      <div className="text-[0.78125rem] text-subtle">
        {line}
        {approval.decided_at ? ` · ${formatTime(approval.decided_at)}` : ''}
        {approval.message ? ` · “${decisionNote(approval.message)}”` : ''}
      </div>
    </>
  )
}

function contentFrom(answer: unknown): Record<string, unknown> {
  const content = (answer as { content?: unknown } | null | undefined)?.content
  return content !== null && typeof content === 'object' && !Array.isArray(content) ? (content as Record<string, unknown>) : {}
}
