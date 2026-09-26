import { createBrowserRouter, redirect } from 'react-router'
import { AppShell } from '@/components/layout/AppShell'
import { HomeRedirect } from '@/components/layout/HomeRedirect'
import { PlaceholderPage } from '@/components/layout/PlaceholderPage'
import { AuthGate } from '@/features/auth/AuthGate'
import { LoginPage } from '@/features/auth/LoginPage'
import { SetupPage } from '@/features/auth/SetupPage'
import { InboxPage } from '@/features/inbox/InboxPage'
import { RoomPage } from '@/features/rooms/RoomPage'
import { AgentsPage } from '@/features/agents/AgentsPage'
import { MachinesPage } from '@/features/machines/MachinesPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { LibraryPage } from '@/features/wiki/LibraryPage'
import { WikisPage } from '@/features/wiki/wikis/WikisPage'

// URL space per docs/webui.md §3. The gate at the root decides between
// the sign-in pages and the app; the app's pages render inside the shell.
export const router = createBrowserRouter([
  {
    path: '/',
    Component: AuthGate,
    children: [
      { path: 'setup', Component: SetupPage },
      { path: 'login', Component: LoginPage },
      {
        Component: AppShell,
        children: [
          { index: true, Component: HomeRedirect },
          { path: 'rooms/:roomId', Component: RoomPage },
          // The chat's Wiki tab: the overview, a page at its own path, the changes.
          { path: 'rooms/:roomId/wiki/*', Component: RoomPage },
          // The chat's Branches tab: the main line and each member's worktree.
          { path: 'rooms/:roomId/branches', Component: RoomPage },
          // Members used to be a page of their own; they are a panel of the
          // chat now (2026-09-16), and the old URL opens it.
          { path: 'rooms/:roomId/agents', loader: ({ params }) => redirect(`/rooms/${params.roomId}?panel=members`) },
          { path: 'inbox', Component: InboxPage },
          // Every project's wiki, or one project's at its pages, beside the
          // chats' Wiki tabs (docs/design.md 5.18).
          { path: 'wiki', Component: WikisPage },
          { path: 'wiki/:projectId/*', Component: WikisPage },
          // The skill library: its front, a page at its own path, the changes.
          { path: 'library/*', Component: LibraryPage },
          { path: 'agents', Component: AgentsPage },
          // The Agents page was at /templates until 2026-09-17.
          { path: 'templates', loader: () => redirect('/agents') },
          { path: 'machines', Component: MachinesPage },
          { path: 'machines/:machineId', Component: MachinesPage },
          // The page was at /workers, then at /runtime (2026-09-16 to
          // 2026-09-17); old links still land.
          { path: 'workers', loader: () => redirect('/machines') },
          { path: 'runtime', loader: () => redirect('/machines') },
          { path: 'runtime/:machineId', loader: ({ params }) => redirect(`/machines/${params.machineId}`) },
          { path: 'settings', Component: SettingsPage },
          { path: 'settings/:section', Component: SettingsPage },
          { path: '*', element: <PlaceholderPage titleKey="page.notFound" /> },
        ],
      },
    ],
  },
])
