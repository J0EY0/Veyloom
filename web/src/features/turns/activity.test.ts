import { describe, expect, it } from 'vitest'
import { formatTime } from '@/lib/format'
import { activityFromEvents, activityFromTranscript, describeTool, summarize, withApprovals, steered } from './activity'

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
    expect(summarize(items)).toBe('读了 1 个文件、跑了 2 条命令、改了 1 个文件、1 个失败')
  })

  it('pairs calls made at once by their ids, whichever ends first', () => {
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'Read', input: '{"file_path":"a.go"}', call_id: 'toolu_1' },
      { kind: 'tool_call', tool: 'Read', input: '{"file_path":"b.go"}', call_id: 'toolu_2' },
      { kind: 'tool_result', tool: 'Read', text: 'package a', call_id: 'toolu_1' },
      { kind: 'tool_call', tool: 'Read', input: '{"file_path":"c.go"}', call_id: 'toolu_3' },
      { kind: 'tool_result', tool: 'Read', text: 'package c', call_id: 'toolu_3' },
    ])
    expect(items).toEqual([
      { kind: 'tool', tool: 'Read', input: '{"file_path":"a.go"}', result: 'package a', status: 'done', callId: 'toolu_1' },
      { kind: 'tool', tool: 'Read', input: '{"file_path":"b.go"}', status: 'running', callId: 'toolu_2' },
      { kind: 'tool', tool: 'Read', input: '{"file_path":"c.go"}', result: 'package c', status: 'done', callId: 'toolu_3' },
    ])
  })

  it('says what the hub was doing as the turn began as done, once something came after', () => {
    const making = { kind: 'notice' as const, level: 'info', text: "Making Coder's worktree, on the branch veyloom/coder." }
    expect(activityFromEvents([making])).toEqual([{ kind: 'notice', level: 'info', text: '正在给 Coder 建工作区，分支 veyloom/coder' }])
    expect(activityFromEvents([making, { kind: 'tool_call', tool: 'Bash', input: '{"command":"ls"}' }])[0]).toMatchObject({
      text: '给 Coder 建了工作区，分支 veyloom/coder',
    })
    expect(activityFromEvents([making], true)[0]).toMatchObject({ text: '给 Coder 建了工作区，分支 veyloom/coder' })
  })

  it('has the call a request holds up wait, the one with its command, and run once allowed', () => {
    // Claude Code asked for two commands at once, and asks about the first.
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'Bash', input: '{"command":"git merge veyloom/coder"}' },
      { kind: 'tool_call', tool: 'Bash', input: '{"command":"git merge veyloom/tester"}' },
    ])
    const asked = { request_id: 'q1', kind: 'tool_use', tool: 'Bash', input: { command: 'git merge veyloom/coder' } }
    const waiting = withApprovals(items, [{ ...asked, status: 'pending' }])
    expect(waiting.map((item) => item.kind === 'tool' && item.status)).toEqual(['waiting', 'running'])
    const allowed = withApprovals(items, [{ ...asked, status: 'allowed' }])
    expect(allowed.map((item) => item.kind === 'tool' && item.status)).toEqual(['running', 'running'])
    // With no command to go by, the latest call of the tool waits.
    const edit = withApprovals(activityFromEvents([{ kind: 'tool_call', tool: 'Write', input: '{"file_path":"a.go"}' }]), [
      { request_id: 'q2', kind: 'tool_use', tool: 'Write', input: { file_path: 'a.go' }, status: 'pending' },
    ])
    expect(edit[0]).toMatchObject({ status: 'waiting' })
  })

  it('says what a turn did by what its tools are for, whichever runtime named them', () => {
    // What the leader's first turn of a trial did (docs/progress.md, step 153).
    const lead = ['ToolSearch', 'Glob', 'mcp__veyloom__search_wiki', 'Read', 'Read', 'mcp__veyloom__send_message'].map((tool) => ({
      kind: 'tool' as const,
      tool,
      input: '',
      status: 'done' as const,
    }))
    expect(summarize(lead)).toBe('查了 wiki、读了 2 个文件、搜了 1 次、派出 1 条消息')
    const pi = ['bash', 'read', 'search_wiki', 'related_wiki', 'edit', 'veyloom/read_topic', 'remember', 'mystery'].map((tool) => ({
      kind: 'tool' as const,
      tool,
      input: '',
      status: 'done' as const,
    }))
    expect(summarize([...pi, { kind: 'file', path: 'store.go' }])).toBe(
      '查了 wiki、看了群聊、读了 1 个文件、跑了 1 条命令、改了 1 个文件、记了 1 条记忆、1 个其他工具调用',
    )
  })

  it('says who a turn was passed a message by mid-turn, and what they said', () => {
    const passed =
      'New in topic #3 while you were at work on it:\n   [system] bob joined the topic\n>> [alice] @Slow use the new grammar\nand keep the old one\n'
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'Read', input: '' },
      { kind: 'steer', text: passed, steer_id: 's1' },
      { kind: 'steer_dropped', steer_id: 's2' },
    ])
    expect(items).toEqual([
      { kind: 'tool', tool: 'Read', input: '', status: 'running' },
      { kind: 'steer', who: 'alice', text: '@Slow use the new grammar' },
    ])
    expect(summarize(items)).toBe('读了 1 个文件、收到 1 条插话')
    expect(steered('New in topic #3:\n>> [alice] first\n>> [bob] second\n')).toEqual({ who: 'bob', text: 'second' })
    expect(steered('nothing marked')).toEqual({ who: '', text: '' })
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
    expect(summarize(items)).toBe('1 次审批、1 次提问')
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

  it('names what other tools work on rather than their JSON', () => {
    expect(describeTool('Read', '{"file_path":"/Users/alice/wt/app/coder/internal/store.go"}')).toBe('Read …/coder/internal/store.go')
    expect(describeTool('Grep', '{"pattern":"TODO","path":"src"}')).toBe('Grep src')
    expect(describeTool('Glob', '{"pattern":"**/*.go"}')).toBe('Glob **/*.go')
    expect(describeTool('fileChange', '{"paths":["a.go","b.go"],"reason":"x"}')).toBe('fileChange a.go b.go')
    expect(describeTool('mcp__veyloom__read_topic', '{"topic":"3"}')).toBe('read_topic 3')
    expect(describeTool('TodoWrite', '{}')).toBe('TodoWrite')
    expect(describeTool('Task', '{"description":"look","subagent_type":"x"}')).toBe('Task {"description":"look","subagent_type":"x"}')
  })

  // A member's reminders to itself (docs/design.md 5.23.4), by any
  // runtime's name for the tools.
  it('says what a reminder asks for, and counts them', () => {
    expect(describeTool('mcp__veyloom__set_reminder', '{"note":"check CI","in":"1h30m"}')).toBe('1 小时 30 分后提醒自己：check CI')
    expect(describeTool('veyloom/set_reminder', '{"note":"check CI","in":"1.5h"}')).toBe('1.5h后提醒自己：check CI')
    const at = '2026-09-28T09:00:00+08:00'
    expect(describeTool('set_reminder', JSON.stringify({ note: 'check CI', at }))).toBe(`${formatTime(at)} 提醒自己：check CI`)
    expect(describeTool('mcp__veyloom__cancel_reminder', '{"id":"rm1"}')).toBe('取消提醒')
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'mcp__veyloom__set_reminder', input: '{"note":"a","in":"1h"}' },
      { kind: 'tool_call', tool: 'veyloom/set_reminder', input: '{"note":"b","in":"2h"}' },
      { kind: 'tool_call', tool: 'cancel_reminder', input: '{"id":"rm1"}' },
    ])
    expect(summarize(items)).toBe('定了 2 个提醒、取消了 1 个提醒')
  })

  // What a member drafts for a person (docs/design.md 5.23.5).
  it('says what a draft has a person do, and counts them', () => {
    expect(describeTool('mcp__veyloom__draft_action', '{"kind":"merge","member":"Coder","message":"Add tags"}')).toBe('起草：把 Coder 的活合进主线')
    expect(describeTool('veyloom/draft_action', '{"kind":"merge","message":"Add tags"}')).toBe('起草：把自己的活合进主线')
    expect(describeTool('draft_action', '{"kind":"set_aside","member":"@Coder","reason":"x"}')).toBe('起草：放弃 Coder 分支上的活')
    expect(describeTool('draft_action', '{"kind":"install_skill","member":"Coder","skill":"go-testing"}')).toBe('起草：给 Coder 装上技能 go-testing')
    expect(describeTool('draft_action', 'not json')).toBe('起草一张卡')
    const items = activityFromEvents([
      { kind: 'tool_call', tool: 'mcp__veyloom__draft_action', input: '{"kind":"merge"}' },
      { kind: 'tool_call', tool: 'draft_action', input: '{"kind":"set_aside"}' },
    ])
    expect(summarize(items)).toBe('起草了 2 张卡')
  })
})
