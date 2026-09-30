import { describe, expect, it } from 'vitest'
import type { InboxItem, PendingApproval } from '@/api/types'
import { approval, message } from '@/test/fixtures'
import { t } from '@/lib/i18n'
import { excerptOf, filterEntries, toEntries } from './entries'

function mention(id: string, seq: number, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    ...message(id, seq, { member_id: 'a1', user_id: undefined }),
    room_name: 'main',
    project_name: 'Veyloom',
    sender_name: 'Pi Tester',
    read: false,
    ...overrides,
  }
}

function pending(id: string, overrides: Partial<PendingApproval> = {}): PendingApproval {
  return { ...approval(id), member_name: 'Careful Builder', project_name: 'docs-site', ...overrides }
}

describe('toEntries', () => {
  it('puts what waits for a decision first and says what it asks for', () => {
    const entries = toEntries([pending('ap1')], [mention('m2', 2, { body: '@alice 看完了', turn_id: 'x2' })], 'alice', t)
    expect(entries.map((entry) => [entry.kind, entry.id, entry.sender, entry.project, entry.excerpt])).toEqual([
      ['approval', 'ap1', 'Careful Builder', 'docs-site', 'make test'],
      ['mention', 'm2', 'Pi Tester', 'Veyloom', '看完了'],
    ])
    expect(entries[0]).toMatchObject({ roomId: 'r1', threadId: 't1' })
    // The answer heading a topic names its turn, not a topic.
    expect(entries[1]).toMatchObject({ threadId: undefined, turnId: 'x2' })
  })

  it('names the files of a mention that has no words', () => {
    const files = [
      { id: 'f1', room_id: 'r1', filename: 'shot.png', media_type: 'image/png', kind: 'image' as const, size: 1, created_at: '' },
      { id: 'f2', room_id: 'r1', filename: 'log.txt', media_type: 'text/plain', kind: 'text' as const, size: 1, created_at: '' },
    ]
    const [entry] = toEntries([], [mention('m1', 1, { body: '@alice', attachments: files })], 'alice', t)
    expect(entry.excerpt).toBe('shot.png、log.txt')
  })

  it('names Veyloom as the sender of its notes, put in the UI words', () => {
    const note = mention('n1', 3, {
      sender_kind: 'system',
      member_id: undefined,
      sender_name: '',
      thread_id: 't1',
      body: '@alice Pong mentioned Ping, but the last 3 turns agents woke in this piece of work only talked; it waits for a person now.',
    })
    const [entry] = toEntries([], [note], 'alice', t)
    expect([entry.sender, entry.excerpt]).toEqual(['Veyloom', 'Pong 想唤醒 Ping，但最近 3 轮被唤醒的 agent 都只回复、没有实际操作，等你决定是否继续'])
  })
})

describe('excerptOf', () => {
  it('drops the leading @ of the reader and the markdown', () => {
    expect(excerptOf('@test\n\n读完了 `web/` 的核心代码。\n\n## 技术栈\n\n- **框架**：React 19\n- *状态*：[TanStack](https://tanstack.com)', 'test')).toBe(
      '读完了 web/ 的核心代码。 技术栈 框架：React 19 状态：TanStack',
    )
  })

  it('keeps an @ of someone else and words with underscores', () => {
    expect(excerptOf('@bob 看 use_mobile 和 snake_case_name', 'test')).toBe('@bob 看 use_mobile 和 snake_case_name')
    expect(excerptOf('@testing 在跑', 'test')).toBe('@testing 在跑')
  })
})

describe('filterEntries', () => {
  const entries = toEntries(
    [pending('ap1')],
    [mention('m1', 1, { body: '@alice 审完了 InboxPage' }), mention('m2', 2, { body: '@alice 构建好了', project_name: 'docs-site' })],
    'alice',
    t,
  )

  it('keeps only approvals when asked', () => {
    expect(filterEntries(entries, '', true).map((entry) => entry.id)).toEqual(['ap1'])
  })

  it('matches every word against names, project and excerpt, ignoring case', () => {
    expect(filterEntries(entries, 'inboxpage', false).map((entry) => entry.id)).toEqual(['m1'])
    expect(filterEntries(entries, 'docs 构建', false).map((entry) => entry.id)).toEqual(['m2'])
    expect(filterEntries(entries, 'careful', false).map((entry) => entry.id)).toEqual(['ap1'])
    expect(filterEntries(entries, 'careful', true).map((entry) => entry.id)).toEqual(['ap1'])
    expect(filterEntries(entries, '没有这个', false)).toEqual([])
  })
})
