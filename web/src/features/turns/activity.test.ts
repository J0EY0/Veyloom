import { describe, expect, it } from 'vitest'
import { activityFromEvents, activityFromTranscript, describeTool, summarize } from './activity'

describe('activity', () => {
  it('pairs tool calls with results and lists files once', () => {
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'Bash', input: '{"command":"go test ./..."}' },
      { kind: 'tool_result', tool: 'Bash', text: 'ok' },
      { kind: 'file_changed', path: 'a.go' },
      { kind: 'file_changed', path: 'a.go' },
      { kind: 'tool_call', tool: 'Bash', input: '{"command":"go vet"}' },
      { kind: 'tool_result', tool: 'Bash', text: 'exit 1: vet failed' },
      { kind: 'tool_call', tool: 'read_file', input: 'README.md' },
    ])
    expect(items).toEqual([
      { kind: 'tool', tool: 'Bash', input: '{"command":"go test ./..."}', result: 'ok', status: 'done' },
      { kind: 'file', path: 'a.go' },
      { kind: 'tool', tool: 'Bash', input: '{"command":"go vet"}', result: 'exit 1: vet failed', status: 'failed' },
      { kind: 'tool', tool: 'read_file', input: 'README.md', status: 'running' },
    ])
    expect(summarize(items)).toBe('改了 1 个文件 · 3 个工具调用 · 1 个失败')
  })

  it('reads a transcript and settles its approvals', () => {
    const items = activityFromTranscript([
      { kind: 'start', runtime: 'fake' },
      { kind: 'event', event: { kind: 'approval_request', tool: 'Bash', input: '{"command":"make test"}' } },
      { kind: 'approval_decision', approval: { id: 'ap1', request_id: 'r1', status: 'allowed' } },
      { kind: 'event', event: { kind: 'text', text: 'built' } },
      { kind: 'done' },
    ])
    expect(items).toEqual([{ kind: 'approval', tool: 'Bash', input: '{"command":"make test"}', status: 'allowed' }])
    expect(summarize(items)).toBe('1 次审批')
  })

  it('describes shell commands as themselves', () => {
    expect(describeTool('Bash', '{"command":"make test"}')).toBe('make test')
    expect(describeTool('read_file', 'README.md')).toBe('read_file README.md')
    expect(describeTool('Bash', 'not json')).toBe('not json')
  })
})
