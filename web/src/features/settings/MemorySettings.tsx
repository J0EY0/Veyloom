import { useSearchParams } from 'react-router'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useMemoryPrefs, useSetMemoryPrefs } from '@/api/settings'
import type { MemoryPrefs } from '@/api/types'
import { personalSpace, useMemory } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'
import { GlobalMemoryDialog } from './GlobalMemoryDialog'
import { SettingRow, SettingsGroup } from './SettingsLayout'

// The memory settings in General (docs/design.md 5.19), after ChatGPT's: a
// switch for memory as a whole and, while it is on, one for each memory.
// The global memory is kept here, in a dialog; each project's in its Wiki.
// The dialog is open while the address says ?memory=personal, so a link
// to the global memory from anywhere opens it.
export function MemorySettings() {
  const t = useT()
  const prefs = useMemoryPrefs()
  const setPrefs = useSetMemoryPrefs()
  const memory = useMemory(personalSpace)
  const [params, setParams] = useSearchParams()
  const open = params.get('memory') === 'personal'
  const setOpen = (next: boolean) =>
    setParams((prev) => {
      const nextParams = new URLSearchParams(prev)
      if (next) nextParams.set('memory', 'personal')
      else nextParams.delete('memory')
      return nextParams
    })
  const value = prefs.data
  const change = (patch: Partial<MemoryPrefs>) => {
    if (value) setPrefs.mutate({ ...value, ...patch }, { onError: (err) => toast.error(t('memory.prefsFailed', { error: errorText(err) })) })
  }

  return (
    <>
      <SettingsGroup title={t('settings.memory')}>
        <SettingRow label={t('memory.enable')} description={t('memory.enableHint')}>
          {value ? (
            <Switch aria-label={t('memory.enable')} checked={value.enabled} onCheckedChange={(enabled) => change({ enabled })} />
          ) : (
            <Skeleton className="h-[1.15rem] w-8 rounded-full" />
          )}
        </SettingRow>
        {value?.enabled ? (
          <SettingRow label={t('memory.global')} description={t('memory.globalHint', { n: memory.data?.entries.length ?? 0 })}>
            <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
              {t('memory.manage')}
            </Button>
            <Switch aria-label={t('memory.global')} checked={value.personal} onCheckedChange={(personal) => change({ personal })} />
          </SettingRow>
        ) : null}
        {value?.enabled ? (
          <SettingRow label={t('wiki.memory')} description={t('memory.projectHint')}>
            <Switch aria-label={t('wiki.memory')} checked={value.project} onCheckedChange={(project) => change({ project })} />
          </SettingRow>
        ) : null}
      </SettingsGroup>
      <GlobalMemoryDialog open={open} onOpenChange={setOpen} />
    </>
  )
}
