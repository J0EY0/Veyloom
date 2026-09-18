import { describe, expect, it } from 'vitest'
import { setLocale } from './i18n'
import { formatTokens, tokenParts, totalTokens } from './tokens'

const usage = { input_tokens: 1234, cache_read_tokens: 150_000, cache_write_tokens: 0, output_tokens: 890 }

describe('tokens', () => {
  it('adds every part up, and nothing when a hub sends none', () => {
    expect(totalTokens(usage)).toBe(152_124)
    expect(totalTokens(undefined)).toBe(0)
  })

  it('says a count the short way of each language', () => {
    expect(formatTokens(152_124)).toBe('15.2万 token')
    expect(formatTokens(9_999)).toBe('9999 token')
    setLocale('en')
    expect(formatTokens(152_124)).toBe('152.1K tokens')
    expect(formatTokens(1)).toBe('1 token')
  })

  it('names each part in full and leaves out the empty ones', () => {
    expect(tokenParts(usage)).toEqual(['输入 1,234', '缓存读取 150,000', '输出 890'])
    setLocale('en')
    expect(tokenParts({ ...usage, cache_write_tokens: 12 })).toEqual(['input 1,234', 'cache read 150,000', 'cache write 12', 'output 890'])
  })
})
