import { useId, useRef, useState } from 'react'
import { CameraIcon } from 'lucide-react'
import { uploadAvatar } from '@/api/avatars'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Button } from '@/components/ui/button'
import { Field, FieldError } from '@/components/ui/field'
import { Spinner } from '@/components/ui/spinner'
import { avatarInputTypes, squareAvatar } from '@/lib/avatarImage'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export interface AvatarFieldProps {
  avatar: string
  // The runtime picked in the dialog, whose mark shows while no picture is.
  runtime: string
  onChange: (avatar: string) => void
  // True while a picked picture is being squared and uploaded.
  onBusyChange: (busy: boolean) => void
}

// The agent's picture beside its name. Clicking it picks an image, which is
// cropped to a square, shrunk and uploaded straight away, so saving only
// names it; taking it off shows the runtime's mark again.
export function AvatarField({ avatar, runtime, onChange, onBusyChange }: AvatarFieldProps) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const id = useId()
  const t = useT()

  async function pick(file: File | undefined) {
    if (!file) return
    if (!avatarInputTypes.includes(file.type)) {
      setError(t('agent.avatarInvalid'))
      return
    }
    setError(undefined)
    setBusy(true)
    onBusyChange(true)
    try {
      onChange(await uploadAvatar(await squareAvatar(file)))
    } catch (err) {
      setError(t('agent.avatarFailed', { error: err instanceof Error ? err.message : String(err) }))
    } finally {
      setBusy(false)
      onBusyChange(false)
      if (input.current) input.current.value = ''
    }
  }

  return (
    <Field data-invalid={error ? true : undefined} className="w-auto flex-none items-center gap-1.5">
      <button
        type="button"
        onClick={() => input.current?.click()}
        disabled={busy}
        aria-label={avatar ? t('agent.avatarChange') : t('agent.avatarUpload')}
        className="group/pick relative rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <AgentAvatar look={{ avatar, runtime }} className="size-16 text-base" />
        <span
          aria-hidden="true"
          className={cn(
            'absolute inset-0 flex items-center justify-center rounded-full bg-black/45 text-white transition-opacity',
            busy ? 'opacity-100' : 'opacity-0 group-hover/pick:opacity-100 group-focus-visible/pick:opacity-100',
          )}
        >
          {busy ? <Spinner className="size-5" /> : <CameraIcon className="size-5" />}
        </span>
      </button>
      <input
        ref={input}
        id={`${id}-avatar`}
        type="file"
        accept={avatarInputTypes.join(',')}
        aria-label={t('agent.avatar')}
        tabIndex={-1}
        className="sr-only"
        onChange={(event) => void pick(event.target.files?.[0])}
      />
      {avatar ? (
        <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs text-subtle" onClick={() => onChange('')} disabled={busy}>
          {t('agent.avatarRemove')}
        </Button>
      ) : null}
      {error ? <FieldError className="max-w-24 text-center text-xs">{error}</FieldError> : null}
    </Field>
  )
}
