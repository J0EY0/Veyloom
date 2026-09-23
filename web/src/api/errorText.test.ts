import { afterEach, describe, expect, it } from 'vitest'
import { setLocale } from '@/lib/i18n'
import { ApiError } from './client'
import { errorText } from './errorText'

describe('errorText', () => {
  afterEach(() => setLocale('zh-CN'))

  it('words a failure the server names by its code, in the language in use', () => {
    const weak = new ApiError(400, 'the password needs at least 8 characters', { error: '…', code: 'weakPassword', params: { min: '8' } })
    expect(errorText(weak)).toBe('密码至少要 8 个字符。')
    setLocale('en')
    expect(errorText(weak)).toBe('A password needs at least 8 characters.')
  })

  it('falls back on the server’s words for a code it does not know', () => {
    expect(errorText(new ApiError(409, 'a turn is still running', { error: 'a turn is still running', code: 'somethingNew' }))).toBe('a turn is still running')
    expect(errorText(new ApiError(400, 'name is required', { error: 'name is required' }))).toBe('name is required')
  })

  it('takes a caller’s words for a status it knows, after any code', () => {
    const raced = new ApiError(409, 'already expired', { error: 'already expired' })
    expect(errorText(raced, { 409: '已经有人决定了' })).toBe('已经有人决定了')
    const expired = new ApiError(409, 'already expired', { error: 'already expired', code: 'approvalExpired', params: { status: 'expired' } })
    expect(errorText(expired, { 409: '已经有人决定了' })).toBe('等得太久，这个请求已经过期了。')
  })

  it('words what is gone, a failing server and one out of reach', () => {
    expect(errorText(new ApiError(404, 'project p1: not found', { error: 'project p1: not found' }))).toBe('找不到了，可能已经被删除。')
    expect(errorText(new ApiError(500, 'internal error', { error: 'internal error' }))).toBe('服务端出错了，详情在服务端的日志里。')
    expect(errorText(new ApiError(502, '502 Bad Gateway'))).toBe('连不上服务端，看看 veyloom serve 是否在运行。')
    expect(errorText(new TypeError('Failed to fetch'))).toBe('连不上服务端，看看 veyloom serve 是否在运行。')
    // A 502 the hub itself sends says why.
    expect(errorText(new ApiError(502, 'the machine could not be reached', { error: 'the machine could not be reached' }))).toBe(
      'the machine could not be reached',
    )
  })
})
