import { useId, useState } from 'react'
import { CheckIcon, FlaskConicalIcon, Undo2Icon } from 'lucide-react'
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
import { actorName } from './names'

// A skill's trial (docs/design.md 5.15). While it is open it sits beside
// the skill's title (SkillTrialHead): how many of the turns it needs have
// used the skill since an agent changed it, how many of them failed, and
// the two ways a person ends it, keeping the change or rolling it back.
// Once over, how it ended is a row of the skill's facts (SkillTrialEnded).
export function SkillTrialHead({ page }: { page: WikiPage }) {
  if (page.trial?.status !== 'open') return null
  return <TrialOpen page={page} trial={page.trial} />
}

// The buttons' face is the page's own colour in both schemes, white in
// light and black in dark: the outline button's dark face is a hair off
// the tag's tint there.
const trialButton = 'px-2.5 dark:border-input dark:bg-background dark:hover:bg-accent'

// TrialOpen is one tag beside the title: how far the trial has got and how
// many of its turns failed, then the two ways to end it, set into the tag
// as plain buttons alike, not as tabs; rolling back asks first. Where the
// tag has no room it breaks in two, the buttons at the end of the second
// line.
function TrialOpen({ page, trial }: { page: WikiPage; trial: SkillTrial }) {
  const t = useT()
  const keep = useChangeWikiPage(librarySpace)
  const [rollingBack, setRollingBack] = useState(false)

  return (
    <div
      role="group"
      aria-label={t('skill.trial.title')}
      className="inline-flex max-w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-status-wait/10 p-0.5 pl-2.5 ring-1 ring-status-wait/20 ring-inset"
    >
      <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium text-status-wait">
        <FlaskConicalIcon className="size-3.5 flex-none" aria-hidden="true" />
        <span>
          {t('skill.trial.open', { uses: trial.uses, needed: trial.needed })}
          {trial.failed > 0 ? (
            <>
              {' · '}
              <span className="text-status-fail">{t('skill.trial.failed', { n: trial.failed })}</span>
            </>
          ) : null}
        </span>
      </span>
      <span className="ml-auto flex items-center gap-1.5">
        <Button
          variant="outline"
          size="xs"
          className={trialButton}
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
          <CheckIcon className="text-status-ok" aria-hidden="true" />
          {t('skill.trial.keep')}
        </Button>
        <Button variant="outline" size="xs" className={trialButton} onClick={() => setRollingBack(true)}>
          <Undo2Icon aria-hidden="true" />
          {t('skill.trial.rollback')}
        </Button>
      </span>
      {rollingBack ? <RollbackDialog name={skillName(page.path)} onClose={() => setRollingBack(false)} /> : null}
    </div>
  )
}

// SkillTrialEnded says how the skill's last trial ended, and when, the way
// the trust row says who wrote it: the words, then the time in grey.
export function SkillTrialEnded({ trial }: { trial: SkillTrial }) {
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
    <>
      <span className="flex min-w-0 items-center gap-1.5">
        <Icon className="size-3.5 flex-none" aria-hidden="true" />
        <span>{text}</span>
      </span>
      {trial.ended_at ? (
        <time dateTime={trial.ended_at} className="text-xs text-subtle">
          {formatTime(trial.ended_at)}
        </time>
      ) : null}
    </>
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
