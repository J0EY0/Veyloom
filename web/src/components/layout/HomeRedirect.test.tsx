import { waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { rememberChat } from '@/lib/lastChat'
import { stubApi } from '@/test/fetch'
import { project } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { HomeRedirect } from './HomeRedirect'

const projects = { projects: [project('p1', 'Veyloom', '', 'r1'), project('p2', 'docs-site', '', 'r2')] }

// The redirect is the only route here, so where it sends you is where the
// router ends up.
function renderHome() {
  return renderWithProviders(<HomeRedirect />, { route: '/', path: '/' })
}

describe('HomeRedirect', () => {
  it('reopens the chat opened last', async () => {
    rememberChat('r2')
    stubApi({ '/projects': projects })
    const { router } = renderHome()
    await waitFor(() => expect(router.state.location.pathname).toBe('/rooms/r2'))
  })

  it('goes to what waits for you when no chat was opened yet, without waiting for the projects', async () => {
    stubApi({ '/projects': () => new Promise(() => {}) })
    const { router } = renderHome()
    await waitFor(() => expect(router.state.location.pathname).toBe('/inbox'))
  })

  it('goes to what waits for you when the chat opened last is gone', async () => {
    rememberChat('r9')
    stubApi({ '/projects': projects })
    const { router } = renderHome()
    await waitFor(() => expect(router.state.location.pathname).toBe('/inbox'))
  })
})
