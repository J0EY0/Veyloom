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

  it("keeps what the runtime told people and takes a reviewer's verdict as settled", () => {
    const items = activityFromEvents([
      { kind: 'notice', level: 'warning', text: 'rate limits are close' },
      { kind: 'notice', text: 'a note' },
      {
        kind: 'approval_request',
        approval_id: 'rv1',
        tool: 'commandExecution',
        input: '{"command":"curl"}',
        reviewer: 'codex_auto_review',
        verdict: 'denied',
        text: 'too risky',
      },
    ])
    expect(items).toEqual([
      { kind: 'notice', level: 'warning', text: 'rate limits are close' },
      { kind: 'notice', level: 'info', text: 'a note' },
      { kind: 'approval', id: 'rv1', tool: 'commandExecution', input: '{"command":"curl"}', status: 'denied', reviewer: 'codex_auto_review' },
    ])
  })

  it('counts questions apart from approvals', () => {
    const items = activityFromEvents([
      { kind: 'approval_request', approval_id: 'q1', approval_kind: 'question', tool: 'AskUserQuestion', input: '{"questions":[]}' },
      { kind: 'approval_request', approval_id: 'a1', tool: 'Bash', input: '{"command":"make"}' },
    ])
    expect(summarize(items)).toBe('1 次审批 · 1 次提问')
    expect(summarize(activityFromEvents([{ kind: 'approval_request', approval_kind: 'form', tool: 'elicitation', input: '{}' }]))).toBe('1 次请求')
  })

  it('settles each decision in a transcript on the request it names', () => {
    const items = activityFromTranscript([
      { kind: 'event', event: { kind: 'approval_request', approval_id: 'r1', tool: 'Bash', input: '{"command":"make test"}' } },
      {
        kind: 'event',
        event: {
          kind: 'approval_request',
          approval_id: 'r2',
          tool: 'commandExecution',
          input: '{"command":"curl"}',
          reviewer: 'codex_auto_review',
          verdict: 'allowed',
        },
      },
      { kind: 'approval_decision', approval: { id: 'ap2', request_id: 'r2', status: 'allowed' } },
    ])
    // The person's request is still waiting: the reviewer's decision is not its.
    expect(items.map((item) => (item.kind === 'approval' ? item.status : item.kind))).toEqual(['pending', 'allowed'])
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
    expect(describeTool('commandExecution', '{"command":"touch a.txt","cwd":"/repo"}')).toBe('touch a.txt')
    expect(describeTool('ExitPlanMode', '{"plan":"\\n## Tidy up\\n1. Remove dead code"}')).toBe('计划：Tidy up')
    expect(describeTool('ExitPlanMode', '{"plan":""}')).toBe('计划：未命名')
    expect(describeTool('SandboxNetworkAccess', '{"host":"example.com"}')).toBe('联网：example.com')
    expect(describeTool('confirm', '{"title":"Dangerous command","message":"Allow rm -rf build?"}')).toBe('确认：Dangerous command — Allow rm -rf build?')
  })
})
