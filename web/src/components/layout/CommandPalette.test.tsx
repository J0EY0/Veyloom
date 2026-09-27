import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { AttachmentViewer } from '@/features/attachments/AttachmentViewer'
import { closeViewer } from '@/features/attachments/viewerStore'
import { setPaletteOpen } from '@/lib/palette'
import { stubApi } from '@/test/fetch'
import { project, room, roomAttachment } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { CommandPalette } from './CommandPalette'

describe('CommandPalette', () => {
  afterEach(() => {
    setPaletteOpen(false)
    act(() => closeViewer())
  })

  it('opens with ⌘K, finds a project by name and goes to its chat', async () => {
    stubApi({
      '/projects': { projects: [project('p1', 'Veyloom', '', 'r1'), project('p2', 'websocket', '', 'r2')] },
    })
    const { router } = renderWithProviders(<CommandPalette />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.keyboard('{Meta>}k{/Meta}')
    const input = await screen.findByPlaceholderText('搜索项目、页面…')
    await userEvent.type(input, 'web')
    await userEvent.click(await screen.findByRole('option', { name: /websocket/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/rooms/r2'))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('lists the pages', async () => {
    stubApi({ '/projects': { projects: [] } })
    renderWithProviders(<CommandPalette />)
    setPaletteOpen(true)
    expect(await screen.findByRole('option', { name: /收件箱/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /机器/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /用量/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /全部项目/ })).not.toBeInTheDocument()
  })

  // In a project's chat the search looks in its attachments too (docs/
  // webui.md 4.21).
  it("finds the open chat's attachments, opens one in the viewer, and shows them all on the tab", async () => {
    const found = Array.from({ length: 7 }, (_, i) => roomAttachment(`a${i}`, `spec-${i}.docx`, 'office'))
    const calls = stubApi({
      '/projects': { projects: [project('p1', 'Veyloom', '', 'r1')] },
      '/rooms/r1': { room: room('r1', 'p1', 'main') },
      '/rooms/r1/attachments': { attachments: found, total: 7 },
    })
    const { router } = renderWithProviders(
      <>
        <CommandPalette />
        <AttachmentViewer />
      </>,
      { route: '/rooms/r1' },
    )
    const user = userEvent.setup()
    act(() => setPaletteOpen(true))
    await user.type(await screen.findByPlaceholderText('搜索项目、页面…'), 'spec')
    expect(await screen.findByText('附件 · Veyloom')).toBeInTheDocument()
    // Five of them, and the way to the rest; nothing else matches, and the
    // first is the one Enter picks.
    expect(screen.getAllByRole('option', { name: /spec-\d\.docx/ })).toHaveLength(5)
    expect(screen.queryByText('没有匹配的结果。')).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: /spec-0\.docx/ })).toHaveAttribute('aria-selected', 'true')
    expect(calls.filter((c) => c.startsWith('GET /rooms/r1/attachments?'))).toEqual(['GET /rooms/r1/attachments?sort=newest&offset=0&limit=60&q=spec'])

    await user.keyboard('{Enter}')
    expect(await screen.findByRole('dialog', { name: 'spec-0.docx' })).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('搜索项目、页面…')).not.toBeInTheDocument()
    act(() => closeViewer())

    act(() => setPaletteOpen(true))
    await user.type(await screen.findByPlaceholderText('搜索项目、页面…'), 'spec')
    await user.click(await screen.findByRole('option', { name: '在附件页里查看全部 7 个结果' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/rooms/r1/attachments'))
    expect(router.state.location.search).toBe('?q=spec')
  })

  it('looks in no attachments outside a chat', async () => {
    const calls = stubApi({ '/projects': { projects: [] } })
    renderWithProviders(<CommandPalette />, { route: '/inbox' })
    act(() => setPaletteOpen(true))
    await userEvent.type(await screen.findByPlaceholderText('搜索项目、页面…'), 'spec')
    expect(await screen.findByText('没有匹配的结果。')).toBeInTheDocument()
    await new Promise((resolve) => setTimeout(resolve, 300))
    expect(calls.filter((c) => c.includes('/attachments'))).toEqual([])
  })
})
