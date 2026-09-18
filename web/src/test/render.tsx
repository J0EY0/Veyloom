import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { createMemoryRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { SidebarProvider } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'

// Renders ui with a fresh query cache and a memory router at route, plus
// the providers the shell gives every page: tooltips and the sidebar
// state. path is the route pattern, for pages that read params.
export function renderWithProviders(ui: ReactNode, { route = '/', path = '*' } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [
      {
        path,
        element: (
          <TooltipProvider>
            <SidebarProvider>{ui}</SidebarProvider>
          </TooltipProvider>
        ),
      },
    ],
    { initialEntries: [route] },
  )
  const result = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  // router.state.location tells a test where a navigation ended up.
  return { ...result, router, client }
}
