import type { InvalidateQueryFilters, QueryClient } from '@tanstack/react-query'

// Queries whose answer cannot change, a PDF opened say, carry settledMeta:
// refresh leaves them be, reading them again being all cost and no news.
export const settledMeta = { settled: true }

// refresh reads queries again because something changed on the hub: an
// event came, or the event stream opened (docs/webui.md §5.3). Invalidating
// alone lets a first read already on its way stand, and that read may have
// been answered before the change, leaving the page on the old answer until
// the next one; so reads on their way are called off, then made again.
export function refresh(client: QueryClient, filters: InvalidateQueryFilters = {}): void {
  const scoped: InvalidateQueryFilters = {
    ...filters,
    predicate: (query) => query.meta?.settled !== true && (filters.predicate?.(query) ?? true),
  }
  void client.cancelQueries(scoped).then(() => client.invalidateQueries(scoped))
}
