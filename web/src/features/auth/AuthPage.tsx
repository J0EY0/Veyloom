import type { ReactNode } from 'react'

// The page around the login-01 block, as the block's own page lays it
// out: one card, centred on an otherwise empty canvas.
export function AuthPage({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
      <div className="w-full max-w-sm">{children}</div>
    </div>
  )
}
