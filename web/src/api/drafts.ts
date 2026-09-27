import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { refreshBranches } from './branches'
import { api } from './client'
import { refresh } from './refresh'
import type { WorkspaceSteps } from './types'

// What a member drafted for a person to do with one press (docs/design.md
// 5.23.5): put a member's work on the main line, give it up, install a
// skill for a member, adopt the setup steps. The card in the topic is drawn
// from it.

export type DraftKind = 'merge' | 'set_aside' | 'install_skill' | 'setup_steps'

// pending: not run yet; running: a person runs it now; conflicted: a merge
// that met conflicts, nothing changed; superseded: drafted anew.
export type DraftStatus = 'pending' | 'running' | 'done' | 'conflicted' | 'declined' | 'superseded'

export interface Draft {
  id: string
  project_id: string
  room_id: string
  thread_id: string
  // Who drafted it, in which turn.
  member_id: string
  turn_id?: string
  kind: DraftKind
  // The member whose work or agent it acts on.
  target_id?: string
  subject: string
  params: { message?: string; reason?: string; skill?: string; steps?: WorkspaceSteps }
  // What the member does once it is done; it is woken then.
  then?: string
  status: DraftStatus
  result: { commit?: string; unsettled?: string; ref?: string; conflicts?: string[] }
  // The card, and the message that told the member what came of it.
  message_id?: string
  result_message_id?: string
  decided_by?: string
  created_at: string
  settled_at?: string
}

export const draftKeys = {
  all: ['drafts'] as const,
  thread: (threadId: string) => ['drafts', threadId] as const,
}

// useThreadDrafts reads the drafts of a topic; only asked for where a note
// is the card of one.
export function useThreadDrafts(threadId: string, enabled: boolean) {
  return useQuery({
    queryKey: draftKeys.thread(threadId),
    queryFn: async () => (await api.get<{ drafts: Draft[] }>(`/threads/${threadId}/drafts`)).drafts,
    enabled: enabled && threadId !== '',
  })
}

// applyDraft writes a draft as it stands now into its topic's list, where
// one is read.
export function applyDraft(client: QueryClient, draft: Draft) {
  const key = draftKeys.thread(draft.thread_id)
  if (client.getQueryData(key) === undefined) return
  client.setQueryData<Draft[]>(key, (list = []) => (list.some((d) => d.id === draft.id) ? list.map((d) => (d.id === draft.id ? draft : d)) : [...list, draft]))
}

// useRunDraft does what a draft has a person do; a merge's message and the
// new files left out, as a person changed them.
export function useRunDraft() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, message, leave }: { id: string; message?: string; leave?: string[] }) =>
      (await api.post<{ draft: Draft }>(`/drafts/${id}/run`, { message: message ?? '', leave: leave ?? [] })).draft,
    onSuccess: (draft) => applyDraft(client, draft),
    // What it did shows on the branches; one refused waits again.
    onSettled: () => {
      refreshBranches(client)
      refresh(client, { queryKey: draftKeys.all })
    },
  })
}

// useDeclineDraft turns a draft down.
export function useDeclineDraft() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => (await api.post<{ draft: Draft }>(`/drafts/${id}/decline`, {})).draft,
    onSuccess: (draft) => applyDraft(client, draft),
    onError: () => refresh(client, { queryKey: draftKeys.all }),
  })
}
