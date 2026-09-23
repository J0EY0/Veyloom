import { zhCN, type MessageKey } from '@/i18n/zh-CN'
import { t } from '@/lib/i18n'
import { ApiError } from './client'

// errorText is what a person is told of a failed request, in their own
// language. A failure the server names by a code has words of its own
// (error.<code> in src/i18n); byStatus gives a caller's words for a status
// it knows the meaning of, such as a 409 on a card someone else decided. A
// server out of reach or failing, and a thing no longer there, are told in
// words of ours. Anything else is told in the server's words, which are
// English.
export function errorText(err: unknown, byStatus?: Partial<Record<number, string>>): string {
  if (err instanceof ApiError) {
    const known = problemText(err.code, err.params) ?? byStatus?.[err.status]
    if (known !== undefined) return known
    if (err.status === 404) return t('error.notFound')
    if (err.status === 500) return t('error.internal')
    // The dev server's proxy answers for a hub that is down, with no JSON.
    if (err.status >= 502 && err.status <= 504 && err.body.error === undefined) return t('error.unreachable')
    return err.message
  }
  // fetch rejects with a TypeError when the server cannot be reached.
  if (err instanceof TypeError) return t('error.unreachable')
  return err instanceof Error ? err.message : String(err)
}

// problemText is a failure the server names by code in the language in use,
// or fallback when the code is none the word lists know.
export function problemText(code: string | undefined, params?: Record<string, string>): string | undefined
export function problemText(code: string | undefined, params: Record<string, string> | undefined, fallback: string): string
export function problemText(code: string | undefined, params?: Record<string, string>, fallback?: string): string | undefined {
  const key = `error.${code ?? ''}`
  return code !== undefined && key in zhCN ? t(key as MessageKey, params) : fallback
}
