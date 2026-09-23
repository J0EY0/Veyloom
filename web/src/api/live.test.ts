import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { patchQuery } from './live'

// A query whose first answer waits until the test lets it go, the way a
// request is on its way when a live event arrives; later answers come at
// once, from what the server holds by then.
function slowFirstAnswer(client: QueryClient, key: string[], first: string[], later: () => string[]) {
  let asked = 0
  let release = () => {}
  const answer = client.fetchQuery({
    queryKey: key,
    queryFn: () => {
      asked++
      return asked === 1 ? new Promise<string[]>((resolve) => (release = () => resolve(first))) : Promise.resolve(later())
    },
  })
  return { answer, release: () => release(), asked: () => asked }
}

describe('patchQuery', () => {
  it('writes an event at once when no request is on its way', () => {
    const client = new QueryClient()
    client.setQueryData(['list'], ['a'])
    patchQuery<string[]>(client, ['list'], (old) => (old ? [...old, 'b'] : old))
    expect(client.getQueryData(['list'])).toEqual(['a', 'b'])
  })

  it('leaves a query that was never read alone', () => {
    const client = new QueryClient()
    patchQuery<string[]>(client, ['list'], (old) => (old ? [...old, 'b'] : old))
    expect(client.getQueryData(['list'])).toBeUndefined()
  })

  it('asks again, once, after an answer that was on its way when events came', async () => {
    const client = new QueryClient()
    const server = ['a']
    const read = slowFirstAnswer(client, ['list'], ['a'], () => [...server])
    // Two events land while the first answer, read before them, travels.
    for (const item of ['b', 'c']) {
      server.push(item)
      patchQuery<string[]>(client, ['list'], (old) => (old ? [...old, item] : old))
    }
    read.release()
    await read.answer
    await vi.waitFor(() => expect(client.getQueryData(['list'])).toEqual(['a', 'b', 'c']))
    expect(read.asked()).toBe(2)
  })
})
