import { useRef, useState } from 'react'
import { PauseIcon, PlayIcon } from 'lucide-react'
import { attachmentUrl } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { Button } from '@/components/ui/button'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { DownloadButton } from './FileCard'

// clock writes seconds as m:ss.
export function clock(seconds: number): string {
  const s = Math.max(0, Math.floor(Number.isFinite(seconds) ? seconds : 0))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

// AudioBar plays a sound file where it is: a button, how far along it is,
// and its length (docs/webui.md 4.21).
export function AudioBar({ attachment, className }: { attachment: Attachment; className?: string }) {
  const t = useT()
  const audio = useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = useState(false)
  const [time, setTime] = useState(0)
  const [length, setLength] = useState(0)

  function toggle() {
    const node = audio.current
    if (!node) return
    if (node.paused) void node.play().catch(() => setPlaying(false))
    else node.pause()
  }

  return (
    <div className={cn('flex w-95 max-w-full items-center gap-3 rounded-xl border bg-card py-2.5 pr-1.5 pl-3', className)}>
      <Button
        type="button"
        size="icon"
        onClick={toggle}
        aria-label={playing ? t('attachment.pause', { name: attachment.filename }) : t('attachment.play', { name: attachment.filename })}
        className="size-8 flex-none rounded-full"
      >
        {playing ? <PauseIcon /> : <PlayIcon className="translate-x-px" />}
      </Button>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <span className="flex items-baseline gap-2">
          <span className="min-w-0 truncate text-[0.8125rem] font-medium text-foreground">{attachment.filename}</span>
          <span className="flex-none text-xs text-subtle tabular-nums">{formatBytes(attachment.size)}</span>
        </span>
        <span className="flex items-center gap-2">
          <input
            type="range"
            min={0}
            max={length || 0}
            step="any"
            value={time}
            disabled={!length}
            aria-label={t('attachment.progress', { name: attachment.filename })}
            onChange={(event) => {
              const next = Number(event.target.value)
              if (audio.current) audio.current.currentTime = next
              setTime(next)
            }}
            className="h-1 min-w-0 flex-1 cursor-pointer accent-foreground disabled:cursor-default"
          />
          <span className="flex-none font-mono text-[0.71875rem] text-subtle tabular-nums">
            {clock(time)} / {clock(length)}
          </span>
        </span>
      </div>
      <DownloadButton attachment={attachment} />
      <audio
        ref={audio}
        src={attachmentUrl(attachment.id)}
        preload="metadata"
        onLoadedMetadata={(event) => setLength(event.currentTarget.duration)}
        onTimeUpdate={(event) => setTime(event.currentTarget.currentTime)}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        className="hidden"
      />
    </div>
  )
}
