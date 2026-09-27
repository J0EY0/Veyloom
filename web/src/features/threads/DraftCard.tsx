import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useRoomMembers } from '@/api/agents'
import { branchesQuery, useBranches } from '@/api/branches'
import { useDeclineDraft, useRunDraft, type Draft } from '@/api/drafts'
import { errorText } from '@/api/errorText'
import type { MemberBranch, Message } from '@/api/types'
import { useUsers } from '@/api/users'
import { NoteLine } from '@/components/shared/note-line'
import { Button } from '@/components/ui/button'
import { Conflicts } from '@/features/branches/Conflicts'
import { StepsText } from '@/features/branches/SetupSteps'
import { MergeDialog } from '@/features/branches/MergeDialog'
import { useT } from '@/lib/i18n'
import { systemNote } from './systemNote'

// DraftCard is the card of something a member drafted for a person to run
// (docs/design.md 5.23.5): what it does, a merge's message or why work is
// given up, and what the member does once it is done. A person runs it with
// one press, changes a merge first, or turns it down. Once settled it says
// what came of it, as quiet as any other note; a merge that met conflicts
// is handed to the member whose work it is.
export function DraftCard({ message, draft }: { message: Message; draft: Draft }) {
  const t = useT()
  const run = useRunDraft()
  const decline = useDeclineDraft()
  const users = useUsers()
  const members = useRoomMembers(draft.room_id, { removed: true })
  const client = useQueryClient()
  // The merge dialog, once the member's branch is read: opening while it is.
  const [opening, setOpening] = useState(false)
  const [editing, setEditing] = useState<{ member: MemberBranch; branch: string }>()
  // Setup steps are shown in the card as they run: the line only says who
  // wrote them down.
  const said = systemNote(t, message.body)
  const note = draft.kind === 'setup_steps' ? { ...said, text: t('draft.stepsNote', { who: said.who[0] ?? '' }) } : said
  const nameOf = (id?: string) => members.data?.find((m) => m.id === id)?.display_name ?? ''
  // The card's own words name the member that drafted it until the
  // members are read.
  const drafter = nameOf(draft.member_id) || note.who[0] || ''
  const target = nameOf(draft.target_id)
  const person = users.data?.find((u) => u.id === draft.decided_by)?.name ?? t('sender.unknownUser')
  const pending = draft.status === 'pending'
  const working = run.isPending || decline.isPending || draft.status === 'running'

  function go() {
    run.mutate({ id: draft.id }, { onError: (err) => toast.error(errorText(err)) })
  }

  async function edit() {
    setOpening(true)
    try {
      const b = await client.fetchQuery(branchesQuery(draft.project_id))
      const member = b.members.find((m) => m.member_id === draft.target_id)
      if (member) setEditing({ member, branch: b.main.branch ?? '' })
      else toast.error(t('draft.noBranch', { name: target }))
    } catch (err) {
      toast.error(errorText(err))
    } finally {
      setOpening(false)
    }
  }

  return (
    <div className="mt-4 first:mt-1">
      <NoteLine note={note} time={message.created_at} settled={!pending && draft.status !== 'running'}>
        {pending || draft.status === 'running' ? null : <span className="flex-none text-xs text-subtle">{outcome(t, draft, person)}</span>}
      </NoteLine>
      <div className="mt-1.5 ml-10 flex flex-col gap-2 rounded-[10px] bg-muted px-3.5 py-3 text-[0.8125rem]">
        {draft.params.message ? (
          <pre className="font-mono text-[0.78125rem] whitespace-pre-wrap text-muted-foreground" translate="no">
            {draft.params.message}
          </pre>
        ) : null}
        {draft.params.reason ? <p className="text-muted-foreground">{draft.params.reason}</p> : null}
        {draft.params.steps ? (
          <p className="text-muted-foreground">
            <StepsText steps={draft.params.steps} />
          </p>
        ) : null}
        {draft.then ? <p className="text-xs text-subtle">{t('draft.then', { who: drafter, then: draft.then })}</p> : null}
        {draft.status === 'conflicted' && draft.target_id ? <DraftConflicts draft={draft} target={target} /> : null}
        {pending || draft.status === 'running' ? (
          <div className="flex flex-wrap gap-2">
            <Button size="xs" disabled={working} onClick={go}>
              {working ? t('draft.running') : t(runLabel[draft.kind])}
            </Button>
            {draft.kind === 'merge' ? (
              <Button size="xs" variant="outline" disabled={working || opening} onClick={() => void edit()}>
                {opening ? t('draft.opening') : t('draft.edit')}
              </Button>
            ) : null}
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              disabled={working}
              onClick={() => decline.mutate(draft.id, { onError: (err) => toast.error(errorText(err)) })}
            >
              {t('draft.decline')}
            </Button>
          </div>
        ) : null}
      </div>
      {editing ? (
        <MergeDialog
          roomId={draft.room_id}
          member={editing.member}
          branch={editing.branch}
          onClose={() => setEditing(undefined)}
          draft={{ id: draft.id, message: draft.params.message ?? '', by: drafter }}
        />
      ) : null}
    </div>
  )
}

// What running each kind of draft is called on its button.
const runLabel = {
  merge: 'draft.merge',
  set_aside: 'draft.setAside',
  install_skill: 'draft.install',
  setup_steps: 'branches.setup.adopt',
} as const

// outcome says what came of a settled draft, by whom.
function outcome(t: ReturnType<typeof useT>, draft: Draft, person: string): string {
  switch (draft.status) {
    case 'done':
      if (draft.kind === 'merge') return t('draft.merged', { who: person, commit: (draft.result.commit ?? '').slice(0, 7) })
      if (draft.kind === 'set_aside') return t('draft.setAsideDone', { who: person })
      if (draft.kind === 'setup_steps') return t('draft.adopted', { who: person })
      return t('draft.done', { who: person })
    case 'conflicted':
      return t('draft.conflicted')
    case 'declined':
      return t('draft.declined', { who: person })
    case 'superseded':
      return t('draft.superseded')
  }
  return ''
}

// DraftConflicts are the files a drafted merge met conflicts in, and the
// press that hands them to the member whose work it is, told to bring the
// main line in.
function DraftConflicts({ draft, target }: { draft: Draft; target: string }) {
  const branches = useBranches(draft.project_id)
  return (
    <Conflicts
      roomId={draft.room_id}
      memberId={draft.target_id ?? ''}
      name={target}
      branch={branches.data?.main.branch ?? ''}
      files={draft.result.conflicts ?? []}
      onDone={() => undefined}
    />
  )
}
