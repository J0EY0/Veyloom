import type { InvalidateQueryFilters, QueryClient } from '@tanstack/react-query'

// refresh reads queries again because something changed on the hub: an
// event came, or the event stream opened (docs/webui.md §5.3). Invalidating
// alone lets a first read already on its way stand, and that read may have
// been answered before the change, leaving the page on the old answer until
// the next one; so reads on their way are called off, then made again.
export function refresh(client: QueryClient, filters: InvalidateQueryFilters = {}): void {
  void client.cancelQueries(filters).then(() => client.invalidateQueries(filters))
}
