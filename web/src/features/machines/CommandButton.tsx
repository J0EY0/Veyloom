import { CopyIcon } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { copyText } from '@/lib/clipboard'
import { useT } from '@/lib/i18n'

// A shell command to run on a machine: the command in monospace on a muted
// chip with a copy mark, and a click copies it.
export function CommandButton({ command }: { command: string }) {
  const t = useT()

  async function copy() {
    if (await copyText(command)) toast.success(t('machines.commandCopied'))
    else toast.error(t('common.copyFailed'))
  }

  return (
    <Button
      type="button"
      variant="secondary"
      size="sm"
      onClick={() => void copy()}
      aria-label={t('machines.copyCommand', { command })}
      className="h-7 max-w-full gap-2 rounded-md px-2.5 font-mono text-xs font-normal text-foreground"
    >
      <span className="truncate" translate="no">
        {command}
      </span>
      <CopyIcon className="size-3.5 text-muted-foreground" />
    </Button>
  )
}
