import { useEffect, useId, useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent } from 'react'
import { useAddressee } from '@/api/addressee'
import { blobFromUrl, uploadAttachment } from '@/api/attachments'
import { usePostMessage } from '@/api/messages'
import {
  PromptInput,
  PromptInputActionAddAttachments,
  PromptInputActionMenu,
  PromptInputActionMenuContent,
  PromptInputActionMenuTrigger,
  PromptInputAttachment,
  PromptInputAttachments,
  PromptInputBody,
  PromptInputFooter,
  PromptInputHeader,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
  usePromptInputAttachments,
  type PromptInputMessage,
} from '@/components/ai-elements/prompt-input'
import { Kbd } from '@/components/ui/kbd'
import { Label } from '@/components/ui/label'
import { registerComposer } from '@/lib/composer'
import { useCurrentUser } from '@/lib/currentUser'
import { draftKey, readDraft, writeDraft } from '@/lib/drafts'
import { composerHint } from './composerHint'
import { MentionPicker } from './MentionPicker'
import { useMentionInput } from './useMentionInput'
import { detectMentions, useMentionTargets } from './useMentionTargets'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

export interface ComposerProps {
  roomId: string
  roomName: string
  // Set to reply inside a topic instead of posting to the room.
  threadId?: string
  // Tighter padding for the topic panel.
  compact?: boolean
}

// Limits mirror the server's: at most this many files, each this big.
const maxFiles = 6
const maxFileMB = 20

// The room's input box, also used to reply in a topic: AI Elements' prompt
// input in its published shape (header for the files picked, body for the
// text, footer with the action menu on the left and submit on the right),
// with the room's @ picker floating above. Enter sends, Shift+Enter breaks
// a line, and Enter while an input method is composing does neither. Files
// come in by the menu, paste or drop; on send they are uploaded first and
// the message carries their ids; while one is going out no more come in.
// Mentions are recomputed from the text so structure and words never
// disagree. Under the text, the box says whom a message without an @ goes
// to, as the hub would route it.
export function Composer({ roomId, roomName, threadId, compact }: ComposerProps) {
  const user = useCurrentUser()
  const post = usePostMessage(roomId)
  const targets = useMentionTargets(roomId)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const mention = useMentionInput(textarea, targets.members)
  const id = useId()
  const pickerId = `${id}-mentions`
  const [error, setError] = useState<string>()
  const [uploading, setUploading] = useState(false)
  // A message is going out, known at once: the busy state the button shows
  // comes a render later.
  const sending = useRef(false)
  const disabled = user === null
  const busy = post.isPending || uploading
  const t = useT()
  const addressee = useAddressee(roomId, threadId)
  // Nothing until the first answer, rather than one hint and then another
  // a moment later; asking again once the stream opens calls off the first
  // try, which leaves the query waiting without fetching for a moment.
  const hint = addressee.isPending ? undefined : composerHint(addressee.data, targets.names, threadId !== undefined, t)

  // What was typed here and not sent comes back with the box: after another
  // tab of the chat, or another chat. Before anything a take-over button
  // hands over below, which goes into it.
  const draft = draftKey(roomId, threadId)
  useLayoutEffect(() => {
    if (textarea.current) textarea.current.value = readDraft(draft)
  }, [draft])

  // A take-over button elsewhere on the page drops its @ in here.
  useEffect(
    () =>
      registerComposer(threadId ?? 'room', (text) => {
        const el = textarea.current
        if (!el) return
        const before = el.value.slice(0, el.selectionStart)
        const after = el.value.slice(el.selectionEnd)
        const glue = before !== '' && !/\s$/.test(before) ? ' ' : ''
        el.value = before + glue + text + after
        writeDraft(draft, el.value)
        const caret = (before + glue + text).length
        el.setSelectionRange(caret, caret)
        el.focus()
      }),
    [threadId, draft],
  )

  // Rejecting tells the prompt input to keep the files for another try.
  async function onSubmit({ text, files }: PromptInputMessage) {
    const el = textarea.current
    if (!el || !user || busy || sending.current) return
    const body = text.trim()
    if (body === '' && files.length === 0) return
    sending.current = true
    setUploading(files.length > 0)
    try {
      const attachmentIds: string[] = []
      for (const file of files) {
        const blob = await blobFromUrl(file.url, file.mediaType ?? '')
        const attachment = await uploadAttachment(roomId, blob, file.filename || 'file')
        attachmentIds.push(attachment.id)
      }
      await post.mutateAsync({
        user_id: user.id,
        body,
        mentions: detectMentions(body, targets.all, targets.names.values()),
        ...(attachmentIds.length > 0 ? { attachment_ids: attachmentIds } : {}),
        ...(threadId ? { thread_id: threadId } : {}),
      })
      writeDraft(draft, '')
      setError(undefined)
      el.focus()
    } catch (err) {
      // The form was cleared on submit; put the words back to retry.
      if (el.value === '') el.value = text
      setError(errorText(err))
      throw err
    } finally {
      sending.current = false
      setUploading(false)
    }
  }

  // While a message is going out, files pasted or dropped are not taken:
  // the prompt input drops every file it holds once the message is sent,
  // and these would go with it, unsent. Words still paste. The menu's
  // button is greyed meanwhile, as the send button is.
  function holdFiles(event: ClipboardEvent<HTMLDivElement> | DragEvent<HTMLDivElement>) {
    if (!busy && !sending.current) return
    const data = 'clipboardData' in event ? event.clipboardData : event.dataTransfer
    const files = Array.from(data?.items ?? []).some((item) => item.kind === 'file') || (data?.files?.length ?? 0) > 0
    if (!files) return
    event.preventDefault()
    event.stopPropagation()
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (mention.onKeyDown(event)) return
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      // While the last message is still going out, Enter waits, as the
      // send button does: submitting clears the box and the files before
      // onSubmit sees it is busy, and both would be lost.
      if (!busy && !sending.current && !disabled) event.currentTarget.form?.requestSubmit()
    }
  }

  return (
    <div className={compact ? 'flex-none px-3 pt-1.5 pb-3' : 'flex-none px-5 pt-2 pb-5'}>
      {/* The picker sits outside the input group, which clips its overflow.
          Files pasted or dropped are looked at here first, on the way in. */}
      <div className="relative mx-auto max-w-205" onPasteCapture={holdFiles} onDropCapture={holdFiles}>
        {mention.open ? <MentionPicker id={pickerId} items={mention.items} active={mention.active} onSelect={mention.select} /> : null}
        <PromptInput
          onSubmit={onSubmit}
          multiple
          maxFiles={maxFiles}
          maxFileSize={maxFileMB << 20}
          onError={(err) =>
            setError(
              err.code === 'max_files'
                ? t('attachment.tooMany', { n: maxFiles })
                : err.code === 'max_file_size'
                  ? t('attachment.tooLarge', { mb: maxFileMB })
                  : err.message,
            )
          }
        >
          <PickedFiles />
          <PromptInputBody>
            <Label htmlFor={id} className="sr-only">
              {t('composer.label')}
            </Label>
            <PromptInputTextarea
              id={id}
              ref={textarea}
              rows={1}
              disabled={disabled}
              placeholder={disabled ? t('common.identifying') : threadId ? t('composer.reply') : t('composer.placeholder', { room: roomName })}
              aria-autocomplete="list"
              aria-controls={mention.open ? pickerId : undefined}
              aria-expanded={mention.open}
              onKeyDown={onKeyDown}
              onInput={(event) => {
                mention.onInput()
                writeDraft(draft, event.currentTarget.value)
              }}
              onBlur={mention.close}
            />
          </PromptInputBody>
          <PromptInputFooter>
            <PromptInputTools className="min-w-0">
              <PromptInputActionMenu>
                <PromptInputActionMenuTrigger aria-label={t('composer.attach')} disabled={disabled || busy} />
                <PromptInputActionMenuContent>
                  <PromptInputActionAddAttachments label={t('composer.attach')} />
                </PromptInputActionMenuContent>
              </PromptInputActionMenu>
              {error ? (
                <span role="alert" className="truncate text-xs font-normal text-status-fail">
                  {error}
                </span>
              ) : (
                <span className="inline-flex min-w-0 items-center gap-1.5 truncate text-xs font-normal text-subtle">
                  {uploading ? t('composer.uploading') : hint?.text}
                  {!uploading && hint?.mention ? <Kbd className="h-4 min-w-4 px-1 text-[0.625rem]">@</Kbd> : null}
                </span>
              )}
            </PromptInputTools>
            <PromptInputSubmit aria-label={t('common.send')} disabled={disabled || busy} aria-busy={busy} status={busy ? 'submitted' : undefined} />
          </PromptInputFooter>
        </PromptInput>
      </div>
    </div>
  )
}

// The files picked so far, above the text as the published prompt input
// draws them; nothing at all while there are none so the header takes no
// room.
function PickedFiles() {
  const attachments = usePromptInputAttachments()
  if (attachments.files.length === 0) return null
  return (
    <PromptInputHeader className="pt-3">
      <PromptInputAttachments className="p-0">{(attachment) => <PromptInputAttachment data={attachment} />}</PromptInputAttachments>
    </PromptInputHeader>
  )
}
