import type { Quota } from '@/api/types'
import type { MessageKey } from '@/i18n/zh-CN'
import { formatTime } from '@/lib/format'
import type { t as translate } from '@/lib/i18n'

type T = typeof translate

// The limits by their spans, as the runtimes name them (docs/design.md
// 5.23.3); any other is shown as it came.
const windows: Record<string, MessageKey> = {
  '5h': 'quota.window.5h',
  '7d': 'quota.window.7d',
  '7d opus': 'quota.window.7dOpus',
  '7d sonnet': 'quota.window.7dSonnet',
  overage: 'quota.window.overage',
}

// quotaText says how a runtime's account stands against its usage limits:
// "5 小时额度已用 85% · 16:23 重置", "7 天额度用完 · 9月30日 16:23 恢复".
export function quotaText(t: T, quota: Quota): string {
  const window = quota.window ? (windows[quota.window] ? t(windows[quota.window]) : quota.window) : ''
  const at = quota.resets_at ? formatTime(quota.resets_at) : ''
  if (quota.limited || (quota.used_percent ?? 0) >= 100) return t('quota.reached', { window }) + (at ? t('quota.back', { time: at }) : '')
  const used = quota.used_percent !== undefined ? t('quota.used', { window, used: quota.used_percent }) : t('quota.known', { window })
  return used + (at ? t('quota.resets', { time: at }) : '')
}
