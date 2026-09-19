import type { Approval } from '@/api/types'

// Claude Code's tool for putting up the plan it made in plan mode for
// approval; its input carries the plan as markdown (docs/design.md 4.6).
export const PLAN_TOOL = 'ExitPlanMode'

// planOf is the markdown of a plan put up for approval, or undefined when
// the approval is something else.
export function planOf(approval: Pick<Approval, 'tool' | 'input'>): string | undefined {
  if (approval.tool !== PLAN_TOOL) return undefined
  let input = approval.input
  if (typeof input === 'string') {
    try {
      input = JSON.parse(input)
    } catch {
      return ''
    }
  }
  const plan = (input as { plan?: unknown } | null)?.plan
  return typeof plan === 'string' ? plan : ''
}

// planTitle names a plan by its first line, headings unmarked.
export function planTitle(plan: string): string {
  for (const line of plan.split('\n')) {
    const text = line
      .trim()
      .replace(/^#+\s*/, '')
      .trim()
    if (text) return text
  }
  return ''
}

// CONFIRM_TOOL is a yes-or-no question one of pi's extensions puts to the
// person; its input is the dialog's title and message.
export const CONFIRM_TOOL = 'confirm'

// confirmationOf reads a yes-or-no question, or undefined when the
// approval is something else.
export function confirmationOf(approval: Pick<Approval, 'tool' | 'input'>): { title: string; message: string } | undefined {
  if (approval.tool !== CONFIRM_TOOL) return undefined
  let input = approval.input
  if (typeof input === 'string') {
    try {
      input = JSON.parse(input)
    } catch {
      return { title: '', message: input as string }
    }
  }
  const { title, message } = (input ?? {}) as { title?: unknown; message?: unknown }
  return { title: typeof title === 'string' ? title : '', message: typeof message === 'string' ? message : '' }
}
