import { useId, useState } from 'react'
import { Link } from 'react-router'
import type { WikiChangeLine, WikiCommit, WikiTeam } from '@/api/types'
import { useRevertWiki, type WikiSpace } from '@/api/wiki'
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
import { plainText } from '@/lib/plainText'
import { useT } from '@/lib/i18n'
import { memoryPage, pageHref } from './links'
import { actorName, changeName, commitNote, reasonOf, subjectText } from './names'
import { errorText } from '@/api/errorText'

export interface CommitRowProps {
  commit: WikiCommit
  space: WikiSpace
  // The wiki keeps a history to undo from.
  canUndo: boolean
  onOpenThread: OpenTopic
  // The teams owning the library's skills, to name the one a skill went to.
  teams?: WikiTeam[]
}

// OpenTopic opens a topic of a chat: the one the wiki is shown in beside
// it, another one in its own chat.
export type OpenTopic = (threadId: string, roomId?: string) => void

// One change in the wiki's history (docs/design.md 5.5): who made it and
// in which topic, when, what it did to which pages, and a way to undo it.
export function CommitRow({ commit, space, canUndo, onOpenThread, teams }: CommitRowProps) {
  const t = useT()
  const [asking, setAsking] = useState(false)
  const note = commitNote(t, commit.subject, teams)
  const outside = commit.changes.some((line) => /(outside Veyloom|while Veyloom was not running)$/.test(line.text))
  return (
    <li className="flex flex-col gap-1 py-3">
      <div className="flex min-w-0 items-center gap-2 text-[0.8125rem]">
        <span className="truncate font-medium">{commit.member || actorName(commit.author)}</span>
        {commit.project_name ? <span className="flex-none text-muted-foreground">{commit.project_name}</span> : null}
        {commit.thread_id && commit.topic_number ? (
          <button
            type="button"
            onClick={() => onOpenThread(commit.thread_id as string, commit.room_id)}
            className="flex-none text-muted-foreground underline-offset-3 hover:text-foreground hover:underline"
          >
            {t('wiki.commit.topic', { n: commit.topic_number })}
          </button>
        ) : null}
        {outside ? <span className="flex-none text-subtle">{t('wiki.commit.outside')}</span> : null}
        <span className="grow" />
        <time dateTime={commit.at} className="flex-none text-xs text-subtle tabular-nums">
          {formatTime(commit.at)}
        </time>
        {canUndo && commit.undoable ? (
          <Button variant="ghost" size="xs" className="-mr-1.5 flex-none text-subtle hover:text-foreground" onClick={() => setAsking(true)}>
            {t('wiki.commit.undo')}
          </Button>
        ) : null}
      </div>
      <ul className="grid gap-0.5 text-[0.8125rem] text-muted-foreground">
        {note ? <li className="min-w-0 break-words">{note}</li> : null}
        {commit.changes.map((line, index) => (
          <li key={index} className="min-w-0 break-words">
            <ChangeLine line={line} space={space} />
          </li>
        ))}
      </ul>
      {asking ? <UndoDialog space={space} sha={commit.sha} onClose={() => setAsking(false)} /> : null}
    </li>
  )
}

function ChangeLine({ line, space }: { line: WikiChangeLine; space: WikiSpace }) {
  const t = useT()
  const undid = /^undid \S+ \((.*)\) by /.exec(line.text)
  const turnedDown = /^\w+ of `([^`]+)` \((.*)\) proposed by ([^:]+?)(?:: (.*))?$/.exec(line.text)
  // A person's reason for undoing, or for rolling a skill back, which the
  // log keeps after who did it.
  const reason = reasonOf(line.text)
  const why = reason ? <span className="text-muted-foreground"> · “{plainText(reason)}”</span> : null
  const rolledBack = line.kind === 'Revert' && / rolled back to \S+ by /.test(line.text)
  const label = <span className="mr-1.5 text-subtle">{rolledBack ? t('wiki.change.rolledBack') : changeName(t, line.kind)}</span>
  if (line.path) {
    // A memory's page has a title of its own in English; the UI names it.
    const memory = line.path === memoryPage && space.kind !== 'library'
    return (
      <>
        {label}
        <Link to={pageHref(space, line.path)} className="text-foreground underline-offset-3 hover:underline">
          {memory ? t(space.kind === 'personal' ? 'memory.global' : 'wiki.memory') : line.title || line.path}
        </Link>
        {why}
      </>
    )
  }
  if (undid)
    return (
      <>
        {t('wiki.commit.undid', { what: subjectText(t, undid[1]) })}
        {why}
      </>
    )
  if (turnedDown) {
    return (
      <>
        {label}
        <span className="text-foreground">{turnedDown[2] || turnedDown[1]}</span>
        {turnedDown[4] ? <span> · “{turnedDown[4]}”</span> : null}
      </>
    )
  }
  return line.kind === 'Initialization' ? label : <>{line.text}</>
}

// Undoing is a new change in the person's name; it waits for the hub, so a
// refusal is read in the dialog. Why, if said, goes into the wiki's log,
// where whoever keeps the wiki reads not to do it again.
function UndoDialog({ space, sha, onClose }: { space: WikiSpace; sha: string; onClose: () => void }) {
  const t = useT()
  const revert = useRevertWiki(space)
  const [reason, setReason] = useState('')
  const reasonId = useId()
  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('wiki.commit.undoTitle')}</AlertDialogTitle>
          <AlertDialogDescription>{t('wiki.commit.undoBody')}</AlertDialogDescription>
        </AlertDialogHeader>
        <Label htmlFor={reasonId} className="sr-only">
          {t('wiki.commit.undoReason')}
        </Label>
        <Input
          id={reasonId}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder={t('wiki.commit.undoReason')}
          autoComplete="off"
          className="text-[0.8125rem]"
        />
        {revert.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {errorText(revert.error, { 409: t('wiki.commit.undoConflict') })}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            disabled={revert.isPending}
            onClick={(event) => {
              event.preventDefault()
              revert.mutate({ sha, reason: reason.trim() }, { onSuccess: onClose })
            }}
          >
            {revert.isPending ? t('wiki.commit.undoing') : t('wiki.commit.undo')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
