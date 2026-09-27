import { describe, expect, it } from 'vitest'
import { accountPause, inEffect, pauseOf } from '@/api/pauses'
import type { Pause } from '@/api/types'
import { quotaText } from '@/features/machines/quota'
import { t } from '@/lib/i18n'
import { pauseLabel } from './memberStatus'

const hour = 3_600_000

function pause(id: string, overrides: Partial<Pause>): Pause {
  return { id, reason: 'quota', detail: '', created_at: '', ...overrides }
}

describe('pauses', () => {
  it('holds a member up by its own pause, else its account’s, until one runs out', () => {
    const now = Date.parse('2026-09-27T08:00:00Z')
    const pauses = [
      pause('own', { member_id: 'a1', reason: 'failing' }),
      pause('account', { machine_id: 'w1', runtime: 'claude', ends_at: new Date(now + hour).toISOString() }),
      pause('over', { machine_id: 'w1', runtime: 'pi', ends_at: new Date(now - 1).toISOString() }),
    ]
    expect(pauseOf(pauses, { id: 'a1', machine_id: 'w1' }, 'claude', now)?.id).toBe('own')
    expect(pauseOf(pauses, { id: 'a2', machine_id: 'w1' }, 'claude', now)?.id).toBe('account')
    expect(pauseOf(pauses, { id: 'a2', machine_id: 'w2' }, 'claude', now)).toBeUndefined()
    expect(pauseOf(pauses, { id: 'a3', machine_id: 'w1' }, 'pi', now)).toBeUndefined()
    expect(pauseOf(pauses, { id: 'a3', machine_id: 'w1' }, undefined, now)).toBeUndefined()
    expect(accountPause(pauses, 'w1', 'claude', now)?.id).toBe('account')
    expect(inEffect(pauses[2], now)).toBe(false)
    expect(inEffect(pauses[0], now)).toBe(true)
  })

  it('says why a member waits, and until when', () => {
    const at = '2026-09-27T08:23:00Z'
    const hhmm = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(at))
    expect(pauseLabel(t, { reason: 'quota' })).toBe('额度用完')
    expect(pauseLabel(t, { reason: 'auth' })).toBe('登录失效')
    expect(pauseLabel(t, { reason: 'failing' })).toBe('连续失败 · 已暂停')
    expect(pauseLabel(t, { reason: 'quota', ends_at: at })).toContain('额度用完 · ')
    expect(pauseLabel(t, { reason: 'rate_limit', ends_at: at })).toContain('被限流 · ')
    expect(pauseLabel(t, { reason: 'server', ends_at: at })).toMatch(/^服务出错 · .+ 重试$/)
    expect(hhmm).toMatch(/\d\d:\d\d/)
  })

  it('says how an account stands against its limits', () => {
    expect(quotaText(t, { window: '5h', used_percent: 85 })).toBe('5 小时额度已用 85%')
    expect(quotaText(t, { window: '7d opus', limited: true })).toBe('7 天 Opus 额度用完')
    expect(quotaText(t, { window: '90m', used_percent: 5 })).toBe('90m额度已用 5%')
    expect(quotaText(t, { window: '5h', used_percent: 100, resets_at: '2026-09-27T08:23:00Z' })).toMatch(/^5 小时额度用完 · .+ 恢复$/)
    expect(quotaText(t, { window: '7d', used_percent: 40, resets_at: '2026-09-27T08:23:00Z' })).toMatch(/^7 天额度已用 40% · .+ 重置$/)
  })
})
