import { QueryClient } from '@tanstack/react-query'
import { ApiError } from '@/api/client'

// The one cache for server state. WebSocket events will write into it too
// (docs/webui.md §5), so REST and realtime never disagree and there is no
// second store to keep in sync.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // A 4xx is an answer, not a hiccup: retrying it only delays the error.
      retry: (failureCount, error) => !(error instanceof ApiError && error.status < 500) && failureCount < 2,
    },
  },
})
