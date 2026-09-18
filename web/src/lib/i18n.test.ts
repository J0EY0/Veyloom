import { describe, expect, it } from 'vitest'
import { en } from '@/i18n/en'
import { zhCN } from '@/i18n/zh-CN'
import { getLocale, setLocale, t } from './i18n'

describe('i18n', () => {
  it('fills placeholders and leaves unknown braces alone', () => {
    setLocale('zh-CN')
    expect(t('topic.replies', { n: 3 })).toBe('3 条回复')
    expect(t('agent.optionsInvalid')).toBe('要是一个 JSON 对象，比如 {"approval": true}。')
    expect(t('nav.projectsFailed', { error: 'boom' })).toBe('项目列表加载失败：boom')
  })

  it('switches language and remembers it', () => {
    setLocale('en')
    expect(getLocale()).toBe('en')
    expect(t('nav.inbox')).toBe('Inbox')
    expect(t('topic.done', { turns: 2, took: t('topic.took', { duration: '38s' }) })).toBe('Done · 2 turns · 38s')
    expect(t('topic.done', { turns: 1, took: '' })).toBe('Done · 1 turn')
    expect(t('topic.replies', { n: 1 })).toBe('1 reply')
    expect(t('activity.tools', { n: 3 })).toBe('3 tool calls')
    expect(localStorage.getItem('veyloom.locale')).toBe('en')
    expect(document.documentElement.lang).toBe('en')
  })

  it('has every key in every language, with the same placeholders', () => {
    const placeholders = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join(',')
    for (const key of Object.keys(zhCN) as (keyof typeof zhCN)[]) {
      expect(en[key], key).toBeTypeOf('string')
      expect(placeholders(en[key]), key).toBe(placeholders(zhCN[key]))
    }
  })
})
