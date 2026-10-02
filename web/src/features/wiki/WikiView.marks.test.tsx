import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { WikiPage } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { catalog, pages, port, renderWiki, routes } from './wikiTesting'

// What a page of the project wiki is marked with besides its text (docs/
// design.md 5.16): where the files it keeps came from, the project memory
// it is not listed with, and whether it is due to be checked again.
describe('WikiView marks', () => {
  it('keeps the project memory, the memory page opening it too', async () => {
    stubApi(
      routes({
        '/projects/p1/memory': { memory: { entries: [{ text: '回复用中文。', date: '2026-09-20', source: 'alice' }], chars: 30, budget: 3000, hash: 'h1' } },
      }),
    )
    for (const rest of ['memory', 'memory.md']) {
      const { unmount } = renderWiki(rest)
      expect(await screen.findByRole('heading', { name: '项目记忆' })).toBeInTheDocument()
      expect(screen.getByText('每一轮都会提供给这个项目的成员。')).toBeInTheDocument()
      expect(await screen.findByText('回复用中文。')).toBeInTheDocument()
      unmount()
    }
  })

  it('says when the project memory is off, and where to turn it on', async () => {
    stubApi(
      routes({
        '/projects/p1/memory': { memory: { entries: [{ text: '回复用中文。' }], chars: 10, budget: 3000, hash: 'h1' } },
        '/settings/memory': { memory: { enabled: true, personal: true, project: false } },
      }),
    )
    renderWiki('memory')
    expect(await screen.findByText(/项目记忆已关闭，不会提供给成员。/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '去设置' })).toHaveAttribute('href', '/settings/general')
    // What it holds is still kept here.
    expect(await screen.findByText('回复用中文。')).toBeInTheDocument()
  })

  it('marks the pages to check again, and says why', async () => {
    const onOpenThread = vi.fn()
    const due: WikiPage = {
      ...port,
      checked_at: '2026-09-01T01:00:00Z',
      review: { why: 'changed', file: 'internal/hub/brief.go', changed_at: '2026-09-20T03:00:00Z', topic_number: 12, thread_id: 't12', room_id: 'r1' },
      // The maintainer confirmed it after it was written: the change
      // counts from then.
      verified_at: '2026-09-21T02:00:00Z',
      verified: [{ by: 'pi/default', at: '2026-09-21T02:00:00Z' }],
    }
    stubApi(
      routes({
        '/projects/p1/wiki': {
          wiki: { ...catalog, pages: [{ ...pages[0], review: due.review, checked_at: due.checked_at, verified_at: due.verified_at }, ...pages.slice(1)] },
        },
        '/projects/p1/wiki/page': { page: due },
      }),
    )
    const { unmount } = renderWiki('')
    expect(await screen.findByText('· 1 页待复核')).toBeInTheDocument()
    // The list marks it, as the graph does, with a dot a screen reader
    // reads as 待复核.
    const row = within(screen.getByLabelText('页面')).getByRole('link', { name: /^The hub listens on 7788/ })
    expect(row).toHaveAccessibleName('The hub listens on 7788待复核')
    expect(row.querySelector('.bg-status-wait')).not.toBeNull()
    const section = screen.getByRole('region', { name: '待复核' })
    expect(within(section).getByRole('link', { name: 'The hub listens on 7788' })).toHaveAttribute('href', '/rooms/r1/wiki/facts/port.md')
    expect(within(section).getByText('internal/hub/brief.go 在上次确认后有改动（9月20日）')).toBeInTheDocument()
    unmount()

    renderWiki('facts/port.md', onOpenThread)
    // One line says it is due and why, with no badge saying it again.
    expect(await screen.findByText(/待复核：internal\/hub\/brief\.go 在上次确认后有改动（9月20日）/)).toBeInTheDocument()
    expect(screen.queryByText('待复核', { selector: '[data-slot="badge"]' })).toBeNull()
    // Who checked it last is among the page's details.
    const details = screen.getByRole('region', { name: '页面信息' })
    expect(within(details).getByText(/^Pi · /)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '话题 #12' }))
    expect(onOpenThread).toHaveBeenCalledWith('t12')
  })

  it('says where each kept file came from: who sent it, when, and in which topic', async () => {
    const onOpenThread = vi.fn()
    const withFiles: WikiPage = {
      ...port,
      sources: [
        {
          id: 'file-1',
          resource: 'veyloom://rooms/r1/messages/m1',
          title: 'diagram.png',
          room_id: 'r1',
          message_id: 'm1',
          sent_by: 'Joey',
          sent_at: '2026-09-23T03:53:00Z',
        },
        {
          id: 'file-2',
          resource: 'veyloom://rooms/r1/messages/m2',
          title: 'checklist.md',
          room_id: 'r1',
          message_id: 'm2',
          sent_by: 'Joey',
          sent_at: '2026-09-23T03:54:00Z',
          thread_id: 't4',
          topic_number: 4,
        },
        { id: 'file-3', resource: 'veyloom://rooms/r1/messages/m3', title: 'old.pdf' },
      ],
    }
    stubApi(routes({ '/projects/p1/wiki/page': { page: withFiles } }))
    renderWiki('facts/port.md', onOpenThread)
    // Said in the chat itself: nowhere further to lead.
    expect(await screen.findByText('diagram.png：Joey 9月23日发在群聊中')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /diagram\.png/ })).not.toBeInTheDocument()
    expect(screen.getByText('old.pdf：原消息已不存在')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'checklist.md：Joey 9月23日发在话题 #4 中' }))
    expect(onOpenThread).toHaveBeenCalledWith('t4')
  })
})
