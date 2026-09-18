import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getCurrentUser, setCurrentUser } from './currentUser'

describe('currentUser', () => {
  beforeEach(() => setCurrentUser(null))

  it('remembers the choice across loads', () => {
    setCurrentUser({ id: 'u1', name: 'alice' })
    expect(getCurrentUser()).toEqual({ id: 'u1', name: 'alice' })
    expect(JSON.parse(localStorage.getItem('veyloom.user') ?? 'null')).toEqual({ v: 1, id: 'u1', name: 'alice' })
  })

  it('forgets on null', () => {
    setCurrentUser({ id: 'u1', name: 'alice' })
    setCurrentUser(null)
    expect(getCurrentUser()).toBeNull()
    expect(localStorage.getItem('veyloom.user')).toBeNull()
  })

  it('ignores a stored value from another schema version', async () => {
    localStorage.setItem('veyloom.user', JSON.stringify({ v: 0, id: 'u1', name: 'alice' }))
    vi.resetModules()
    const fresh = await import('./currentUser')
    expect(fresh.getCurrentUser()).toBeNull()
  })
})
