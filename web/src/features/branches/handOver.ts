import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { usePostMessage } from '@/api/messages'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'

// useHandOver hands a member a job about its worktree (docs/design.md
// 5.21): a message in the chat, from the person, that mentions it.
export function useHandOver(roomId: string, memberId: string, name: string) {
  const t = useT()
  const user = useCurrentUser()
  const post = usePostMessage(roomId)

  function handOver(text: string, onDone?: () => void) {
    if (!user) return
    post.mutate(
      { user_id: user.id, body: `@${name} ${text}`, mentions: [{ kind: 'agent', id: memberId }] },
      {
        onSuccess: () => {
          toast.success(t('branches.handedOver', { name }))
          onDone?.()
        },
        onError: (err) => toast.error(errorText(err)),
      },
    )
  }

  return { handOver, pending: post.isPending }
}
