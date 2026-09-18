import type { TokenUsage } from '@/api/types'
import { formatCompactCount, formatCount } from './format'
import { t } from './i18n'

// totalTokens is every token spent, all parts together; nothing when a
// hub from before usage was kept sends none.
export function totalTokens(usage: TokenUsage | undefined): number {
  if (!usage) return 0
  return usage.input_tokens + usage.cache_read_tokens + usage.cache_write_tokens + usage.output_tokens
}

// formatTokens is a token count for a glance: "12.3万 token".
export function formatTokens(n: number): string {
  return t('tokens.count', { count: formatCompactCount(n), n })
}

// tokenParts says what a usage is made of, in full, one phrase per part
// that is not zero: fresh input, cache read, cache write, output.
export function tokenParts(usage: TokenUsage): string[] {
  const parts = [
    ['tokens.input', usage.input_tokens],
    ['tokens.cacheRead', usage.cache_read_tokens],
    ['tokens.cacheWrite', usage.cache_write_tokens],
    ['tokens.output', usage.output_tokens],
  ] as const
  return parts.filter(([, n]) => n > 0).map(([key, n]) => t(key, { count: formatCount(n) }))
}
