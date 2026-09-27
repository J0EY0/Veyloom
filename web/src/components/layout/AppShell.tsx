import { Outlet, useLocation } from 'react-router'
import { useInboxEvents } from '@/api/inbox'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'
import { AppSidebar } from './AppSidebar'
import { CommandPalette } from './CommandPalette'
import { AttachmentViewer } from '@/features/attachments/AttachmentViewer'
import { ErrorBoundary } from './ErrorBoundary'

// Window layout: the sidebar sits on the canvas, pages render as a card
// with an 8px margin (docs/webui.md §0). shadcn's inset sidebar draws
// exactly that, and folds away with ⌘B. What reaches the person streams in
// for as long as the app is open, wherever they are in it.
export function AppShell() {
  const location = useLocation()
  const t = useT()
  useInboxEvents(useCurrentUser()?.id ?? '')
  return (
    <TooltipProvider delayDuration={300}>
      <SidebarProvider style={{ '--sidebar-width': '15rem' } as React.CSSProperties} className="h-dvh min-h-0">
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-popover focus:px-3 focus:py-1.5 focus:shadow-pop"
        >
          {t('common.skipToContent')}
        </a>
        <AppSidebar />
        <SidebarInset
          id="main"
          // The inset gains its left margin as the sidebar folds away; easing it like the sidebar keeps the card from jumping first.
          className="min-h-0 overflow-hidden border shadow-card transition-[margin] duration-200 ease-linear md:peer-data-[variant=inset]:my-2 md:peer-data-[variant=inset]:mr-2"
        >
          <ErrorBoundary resetKey={location.pathname}>
            <Outlet />
          </ErrorBoundary>
        </SidebarInset>
        <CommandPalette />
        <AttachmentViewer />
        <Toaster position="bottom-right" />
      </SidebarProvider>
    </TooltipProvider>
  )
}
