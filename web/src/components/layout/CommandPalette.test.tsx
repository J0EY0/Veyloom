import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { setPaletteOpen } from '@/lib/palette'
import { stubApi } from '@/test/fetch'
import { project } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { CommandPalette } from './CommandPalette'

describe('CommandPalette', () => {
  afterEach(() => setPaletteOpen(false))

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
    expect(screen.queryByRole('option', { name: /全部项目/ })).not.toBeInTheDocument()
  })
})
