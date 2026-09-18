import { describe, expect, it } from 'vitest'
import type { InboxItem, PendingApproval } from '@/api/types'
import { approval, message } from '@/test/fixtures'
import { excerptOf, filterEntries, plainText, toEntries } from './entries'

function mention(id: string, seq: number, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    ...message(id, seq, { member_id: 'a1', user_id: undefined }),
    room_name: 'main',
    project_name: 'Veyloom',
    sender_name: 'Pi Tester',
    ...overrides,
  }
}

function pending(id: string, overrides: Partial<PendingApproval> = {}): PendingApproval {
  return { ...approval(id), member_name: 'Careful Builder', project_name: 'docs-site', ...overrides }
}

describe('toEntries', () => {
  it('puts what waits for a decision first and says what it asks for', () => {
    const entries = toEntries([pending('ap1')], [mention('m2', 2, { body: '@alice 看完了', turn_id: 'x2' })], 'alice')
    expect(entries.map((entry) => [entry.kind, entry.id, entry.sender, entry.project, entry.excerpt])).toEqual([
      ['approval', 'ap1', 'Careful Builder', 'docs-site', 'make test'],
      ['mention', 'm2', 'Pi Tester', 'Veyloom', '看完了'],
    ])
    expect(entries[0]).toMatchObject({ roomId: 'r1', threadId: 't1' })
    // A closing message names its turn, not a topic.
    expect(entries[1]).toMatchObject({ threadId: undefined, turnId: 'x2' })
  })

  it('names the files of a mention that has no words', () => {
    const files = [
      { id: 'f1', room_id: 'r1', filename: 'shot.png', media_type: 'image/png', size: 1, created_at: '' },
      { id: 'f2', room_id: 'r1', filename: 'log.txt', media_type: 'text/plain', size: 1, created_at: '' },
    ]
    const [entry] = toEntries([], [mention('m1', 1, { body: '@alice', attachments: files })], 'alice')
    expect(entry.excerpt).toBe('shot.png、log.txt')
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

describe('plainText', () => {
  it('flattens fences, quotes, tables and rules', () => {
    expect(plainText('> 引用\n\n```go\nfmt.Println(1)\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n1. 第一')).toBe('引用 fmt.Println(1) a b 1 2 第一')
  })
})

describe('filterEntries', () => {
  const entries = toEntries(
    [pending('ap1')],
    [mention('m1', 1, { body: '@alice 审完了 InboxPage' }), mention('m2', 2, { body: '@alice 构建好了', project_name: 'docs-site' })],
    'alice',
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
