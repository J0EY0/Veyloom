import { MessageCircleQuestionIcon } from 'lucide-react'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import type { WikiPage } from '@/api/types'
import { useWikiQuestion } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { queueForComposer } from '@/lib/composer'
import { useT } from '@/lib/i18n'
import type { OpenTopic } from './CommitRow'

// QuestionButton puts a person's doubt about a page of the project's wiki
// to whoever keeps it (docs/design.md 5.15): the wiki topic opens beside
// the page, its box already naming them and the page, for the person to
// say what is wrong.
export function QuestionButton({ page, projectId, onOpenThread }: { page: WikiPage; projectId: string; onOpenThread: OpenTopic }) {
  const t = useT()
  const ask = useWikiQuestion(projectId)
  return (
    <Button
      variant="ghost"
      size="xs"
      className="text-muted-foreground"
      disabled={ask.isPending}
      onClick={() =>
        ask.mutate(undefined, {
          onSuccess: (question) => {
            queueForComposer(question.thread_id, t('wiki.page.askText', { name: question.member_name, title: page.title || page.path, path: page.path }))
            onOpenThread(question.thread_id)
          },
          onError: (err) => toast.error(t('wiki.page.askFailed', { error: errorText(err) })),
        })
      }
    >
      <MessageCircleQuestionIcon />
      {t('wiki.page.ask')}
    </Button>
  )
}
