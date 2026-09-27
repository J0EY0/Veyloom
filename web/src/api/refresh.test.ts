import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import { refresh, settledMeta } from './refresh'

// A query on screen whose first read is still on its way, answered from
// before a change, and read again after it.
function watched() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const answers: ((value: string) => void)[] = []
  const observer = new QueryObserver(client, {
    queryKey: ['upkeep', 'p1'],
    queryFn: () => new Promise<string>((resolve) => answers.push(resolve)),
  })
  const unsubscribe = observer.subscribe(() => {})
  return { client, answers, observer, unsubscribe }
}

describe('refresh', () => {
  it('reads again a first read already on its way, which invalidating alone lets stand', async () => {
    const plain = watched()
    void plain.client.invalidateQueries({ queryKey: ['upkeep'] })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(plain.answers).toHaveLength(1)
    plain.unsubscribe()

    const { client, answers, observer, unsubscribe } = watched()
    refresh(client, { queryKey: ['upkeep'] })
    await expect.poll(() => answers.length).toBe(2)
    // The first read answers late, from before the change: it is not used.
    answers[0]('nothing waits')
    answers[1]('a turn waits')
    await expect.poll(() => observer.getCurrentResult().data).toBe('a turn waits')
    unsubscribe()
  })

  it('leaves be what cannot change, and keeps to the filters given', async () => {
    const client = new QueryClient()
    await client.fetchQuery({ queryKey: ['rooms', 'r1'], queryFn: () => 'room' })
    await client.fetchQuery({ queryKey: ['tasks', 'r1'], queryFn: () => 'tasks' })
    await client.fetchQuery({ queryKey: ['pdf', '/api/v1/attachments/a'], queryFn: () => 'doc', meta: settledMeta })
    const invalidated = (key: string[]) => client.getQueryState(key)?.isInvalidated

    refresh(client, { queryKey: ['rooms'] })
    await expect.poll(() => invalidated(['rooms', 'r1'])).toBe(true)
    expect(invalidated(['tasks', 'r1'])).toBe(false)

    refresh(client)
    await expect.poll(() => invalidated(['tasks', 'r1'])).toBe(true)
    expect(invalidated(['pdf', '/api/v1/attachments/a'])).toBe(false)
  })
})
