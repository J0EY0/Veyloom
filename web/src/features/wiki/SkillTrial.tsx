import { useId, useState } from 'react'
import { CheckIcon, FlaskConicalIcon, Undo2Icon } from 'lucide-react'
import { Link } from 'react-router'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import type { SkillTrial, WikiPage } from '@/api/types'
import { librarySpace, skillName, useChangeWikiPage, useRollbackSkill } from '@/api/wiki'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { topicHref } from './links'
import { actorName } from './names'

// A skill's trial (docs/design.md 5.15). While it is open: how the turns
// that used the skill since an agent changed it went, where the change was
// made, and the two ways a person ends it, keeping the change or rolling
// it back. Once over, one line on how it ended.
export function SkillTrialPart({ page }: { page: WikiPage }) {
  if (!page.trial) return null
  return page.trial.status === 'open' ? <TrialOpen page={page} trial={page.trial} /> : <TrialEnded trial={page.trial} />
}

function TrialOpen({ page, trial }: { page: WikiPage; trial: SkillTrial }) {
  const t = useT()
  const keep = useChangeWikiPage(librarySpace)
  const [rollingBack, setRollingBack] = useState(false)
  const left = Math.max(trial.needed - trial.uses, 0)

  return (
    <section aria-label={t('skill.trial.title')} className="mt-4 rounded-lg border border-status-wait/30 bg-status-wait/5 px-3.5 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <p className="flex items-center gap-1.5 text-[0.8125rem] font-medium text-status-wait">
          <FlaskConicalIcon className="size-3.5" aria-hidden="true" />
          {t('skill.trial.open', { uses: trial.uses, needed: trial.needed })}
        </p>
        {trial.failed > 0 ? <span className="text-xs text-status-fail">{t('skill.trial.failed', { n: trial.failed })}</span> : null}
        <span className="grow" />
        <Button
          variant="outline"
          size="xs"
          disabled={keep.isPending}
          onClick={() =>
            keep.mutate(
              { path: page.path, verify: true },
              {
                onSuccess: () => toast.success(t('skill.trial.kept')),
                onError: (err) => toast.error(t('skill.trial.keepFailed', { error: errorText(err) })),
              },
            )
          }
        >
          {t('skill.trial.keep')}
        </Button>
        <Button variant="ghost" size="xs" onClick={() => setRollingBack(true)}>
          {t('skill.trial.rollback')}
        </Button>
      </div>
      <p className="mt-1.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
        <span>{t('skill.trial.changedBy', { who: trial.changed_by })}</span>
        <span aria-hidden="true">·</span>
        {trial.room_id && trial.thread_id ? (
          <Link to={topicHref(trial.room_id, trial.thread_id)} className="underline-offset-3 hover:underline">
            {t('skill.use', { project: trial.project_name, n: trial.topic_number ?? 0 })}
          </Link>
        ) : (
          <span>{trial.project_name}</span>
        )}
        <span aria-hidden="true">·</span>
        <time dateTime={trial.changed_at}>{formatTime(trial.changed_at)}</time>
        {trial.changes > 1 ? <span>{t('skill.trial.changes', { n: trial.changes })}</span> : null}
      </p>
      <p className="mt-1 text-xs text-subtle">{t('skill.trial.hint', { n: left })}</p>
      {rollingBack ? <RollbackDialog name={skillName(page.path)} onClose={() => setRollingBack(false)} /> : null}
    </section>
  )
}

// TrialEnded says how the skill's last trial ended.
function TrialEnded({ trial }: { trial: SkillTrial }) {
  const t = useT()
  const by = trial.ended_by ?? ''
  let text: string
  if (trial.status === 'rolled_back') {
    text = t('skill.trial.rolledBack', { who: actorName(by) }) + (trial.reason ? t('skill.trial.because', { reason: trial.reason }) : '')
  } else if (by.startsWith('process:')) {
    text = t('skill.trial.keptAuto')
  } else if (trial.reason === 'changed by hand') {
    text = t('skill.trial.keptByHand', { who: actorName(by) })
  } else {
    text = t('skill.trial.keptBy', { who: actorName(by) })
  }
  const Icon = trial.status === 'rolled_back' ? Undo2Icon : CheckIcon
  return (
    <p className="mt-3 flex flex-wrap items-center gap-x-1.5 text-xs text-subtle">
      <Icon className="size-3.5" aria-hidden="true" />
      <span>{text}</span>
      {trial.ended_at ? (
        <>
          <span aria-hidden="true">·</span>
          <time dateTime={trial.ended_at}>{formatTime(trial.ended_at)}</time>
        </>
      ) : null}
    </p>
  )
}

// RollbackDialog asks before a skill goes back to its version before the
// trial, with a reason for the log that may stay empty.
function RollbackDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const t = useT()
  const rollback = useRollbackSkill()
  const [reason, setReason] = useState('')
  const reasonId = useId()
  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('skill.trial.rollbackTitle')}</AlertDialogTitle>
          <AlertDialogDescription>{t('skill.trial.rollbackBody')}</AlertDialogDescription>
        </AlertDialogHeader>
        <Label htmlFor={reasonId} className="sr-only">
          {t('skill.trial.rollbackReason')}
        </Label>
        <Input
          id={reasonId}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder={t('skill.trial.rollbackReason')}
          autoComplete="off"
          className="text-[0.8125rem]"
        />
        {rollback.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {errorText(rollback.error)}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            disabled={rollback.isPending}
            onClick={(event) => {
              event.preventDefault()
              rollback.mutate(
                { name, reason: reason.trim() },
                {
                  onSuccess: () => {
                    toast.success(t('skill.trial.rolledBackToast'))
                    onClose()
                  },
                },
              )
            }}
          >
            {rollback.isPending ? t('skill.trial.rollingBack') : t('skill.trial.rollbackDo')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
