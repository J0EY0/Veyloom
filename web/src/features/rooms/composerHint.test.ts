import { describe, expect, it } from 'vitest'
import type { Addressee } from '@/api/types'
import { t } from '@/lib/i18n'
import { composerHint } from './composerHint'

const names = new Map([
  ['a1', 'Lead'],
  ['a2', 'Coder'],
])

describe('composerHint', () => {
  it('names whom a message without an @ goes to, and why', () => {
    const cases: [Addressee, string][] = [
      [{ member_id: 'a2', reason: 'only' }, '不带 @ 时由 Coder 回复'],
      [{ member_id: 'a1', reason: 'leader' }, '不带 @ 时交给组长 Lead 分派'],
      [{ member_id: 'a2', reason: 'running' }, '不带 @ 时由 Coder 回复'],
      [{ member_id: 'a2', reason: 'talking' }, '不带 @ 时由 Coder 回复'],
      [{ member_id: 'a2', reason: 'last' }, '不带 @ 时由 Coder 回复'],
      [{ member_id: 'a1', reason: 'leader_fallback' }, '不带 @ 时交给组长 Lead'],
    ]
    for (const [to, text] of cases) {
      expect(composerHint(to, names, true, t), to.reason).toEqual({ text })
    }
  })

  it('asks for an @ where a message without one reaches nobody', () => {
    for (const inTopic of [false, true]) {
      expect(composerHint({ reason: 'none' }, names, inTopic, t)).toEqual({ text: '输入 @ 提到成员', mention: true })
    }
  })

  it('keeps to the plain hints until it knows whom, and by what name', () => {
    for (const to of [undefined, { member_id: 'gone', reason: 'last' } as Addressee]) {
      expect(composerHint(to, names, false, t)).toEqual({ text: '输入 @ 提到成员', mention: true })
      expect(composerHint(to, names, true, t)).toEqual({ text: 'Enter 发送，Shift+Enter 换行' })
    }
  })
})
