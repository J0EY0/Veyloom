import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'
import { useHandOver } from './handOver'

export interface ConflictsProps {
  roomId: string
  memberId: string
  name: string
  // The main line's branch, as the member is told to merge it in.
  branch: string
  files: string[]
  // After handing it over.
  onDone: () => void
}

// Conflicts lists the files a member's work and the main line both changed
// in ways git cannot put together, and hands the member the job of
// resolving them (docs/design.md 5.21): a message in the chat, from the
// person, asking it to bring the main line in and settle them.
export function Conflicts({ roomId, memberId, name, branch, files, onDone }: ConflictsProps) {
  const t = useT()
  const { handOver, pending } = useHandOver(roomId, memberId, name)

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[0.8125rem] text-status-fail">{t('branches.conflicts', { name })}</p>
      <ul className="flex flex-col gap-0.5 font-mono text-[0.78125rem]" translate="no">
        {files.map((file) => (
          <li key={file} className="break-all">
            {file}
          </li>
        ))}
      </ul>
      <div>
        <Button
          size="sm"
          disabled={pending}
          onClick={() => handOver(t('branches.handOverMessage', { branch, files: files.join(t('common.listSeparator')) }), onDone)}
        >
          {t('branches.handOver', { name })}
        </Button>
      </div>
    </div>
  )
}
