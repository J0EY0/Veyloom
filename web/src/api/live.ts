import type { QueryClient, QueryKey } from '@tanstack/react-query'

// The queries of each client to ask again once the answer on its way is in.
const followUps = new WeakMap<QueryClient, Set<string>>()

// patchQuery writes a live event into what one query holds (docs/webui.md
// §5.2). An event can arrive while a request for the query is on its way,
// one the server may have answered before the event happened: when that
// answer lands it puts back the state before the event, and the event is
// lost. So while a request is on its way the event is not written; the
// query is asked again once the answer is in, and the new answer has it.
// Events arriving meanwhile share that one request.
export function patchQuery<T>(client: QueryClient, queryKey: QueryKey, update: (old: T | undefined) => T | undefined) {
  const query = client.getQueryCache().find<T>({ queryKey, exact: true })
  const onItsWay = query?.state.fetchStatus === 'fetching' ? query.promise : undefined
  if (!query || !onItsWay) {
    client.setQueryData<T>(queryKey, update)
    return
  }
  const waiting = followUps.get(client) ?? new Set<string>()
  followUps.set(client, waiting)
  if (waiting.has(query.queryHash)) return
  waiting.add(query.queryHash)
  void onItsWay
    .catch(() => undefined)
    .finally(() => {
      waiting.delete(query.queryHash)
      void client.refetchQueries({ queryKey, exact: true })
    })
}
