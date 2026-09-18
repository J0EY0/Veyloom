import type { Approval } from '@/api/types'
import { describeTool } from '@/features/turns/activity'

// approvalCommand says what an approval asks for in one line: the shell
// command itself, or the tool and its input.
export function approvalCommand(approval: Pick<Approval, 'tool' | 'input'>): string {
  const input = typeof approval.input === 'string' ? approval.input : JSON.stringify(approval.input ?? '')
  return describeTool(approval.tool, input)
}
