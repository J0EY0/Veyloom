import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useLiftPause } from '@/api/pauses'
import type { Pause, Quota } from '@/api/types'
import { RuntimeIcon } from '@/components/shared/runtime-icon'
import type { StatusTone } from '@/components/shared/status-dot'
import { StatusPill } from '@/components/shared/status-pill'
import { Button } from '@/components/ui/button'
import { Item, ItemActions, ItemContent, ItemMedia, ItemTitle } from '@/components/ui/item'
import { pauseLabel } from '@/features/rooms/memberStatus'
import type { MessageKey } from '@/i18n/zh-CN'
import { runtimeName } from '@/lib/runtimes'
import { useT } from '@/lib/i18n'
import type { DetectedRuntime, RuntimeState } from './machines'
import { quotaText } from './quota'

const pills: Record<RuntimeState, { tone: StatusTone; key: MessageKey }> = {
  ready: { tone: 'ok', key: 'runtime.ready' },
  unconfigured: { tone: 'wait', key: 'runtime.unconfigured' },
  failed: { tone: 'fail', key: 'runtime.failed' },
}

// One runtime a machine found, in one row: its mark, name and version with
// where it lives under them, in full (a long path wraps rather than hiding
// behind a hover), how its account stands against its usage limits, and on
// the right its state: a pause holding its members up, with the way on
// (docs/design.md 5.23.3). In a narrow pane the right side drops under the
// name.
export function RuntimeItem({ runtime, quota, pause }: { runtime: DetectedRuntime; quota?: Quota; pause?: Pause }) {
  const t = useT()
  const lift = useLiftPause()
  const { info, state } = runtime
  const pill = pills[state]
  return (
    <Item role="listitem" size="sm" className="flex-wrap gap-x-3 gap-y-2 py-3 @xl:flex-nowrap">
      <ItemMedia>
        <RuntimeIcon runtime={info.name} className="size-8" />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-0.5">
        <ItemTitle className="gap-2">
          <span translate="no">{runtimeName(info.name)}</span>
          {info.version ? (
            <span className="font-mono text-xs font-normal text-subtle" translate="no">
              {info.version}
            </span>
          ) : null}
        </ItemTitle>
        {info.path ? (
          <p className="font-mono text-[0.71875rem] break-all text-subtle" translate="no">
            {info.path}
          </p>
        ) : null}
        {state === 'failed' ? <p className="text-xs text-subtle">{t('machines.hintFailed', { detail: info.detail || info.status })}</p> : null}
        {quota ? <p className="text-xs text-subtle">{quotaText(t, quota)}</p> : null}
      </ItemContent>
      <ItemActions className="basis-full flex-wrap gap-x-3 gap-y-2 pl-11 @xl:basis-auto @xl:flex-nowrap @xl:pl-0">
        {pause ? (
          <>
            <StatusPill tone="wait">{pauseLabel(t, pause)}</StatusPill>
            <Button
              size="xs"
              variant="outline"
              disabled={lift.isPending}
              onClick={() => lift.mutate(pause.id, { onError: (err) => toast.error(errorText(err)) })}
            >
              {t('pause.resume')}
            </Button>
          </>
        ) : (
          <StatusPill tone={pill.tone}>{t(pill.key)}</StatusPill>
        )}
      </ItemActions>
    </Item>
  )
}
