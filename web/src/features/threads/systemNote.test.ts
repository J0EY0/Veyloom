import { describe, expect, it } from 'vitest'
import { formatTime } from '@/lib/format'
import { t } from '@/lib/i18n'
import { isHoldNote, isReminderNote, noticeText, spanMs, systemNote, systemText } from './systemNote'

// The notes of a project's setup and of its branches, in the UI's words
// (docs/design.md 5.21).
describe('systemText of the setup and the branches', () => {
  it('says who sets the project up, and why', () => {
    expect(systemText(t, 'Project setup by Lead (a member needs a worktree).')).toBe('Lead 开始初始化项目（有员工要开工作区）')
    expect(systemText(t, 'Project setup by Lead (asked by a person).')).toBe('Lead 开始初始化项目（有人让初始化）')
  })

  it('says what steps wait for a person', () => {
    expect(
      systemText(t, 'Lead wrote down how a new worktree is got ready: copy .env, docs/, then run npm ci. A person adopts the command before it runs.'),
    ).toBe('Lead 写好了新工作区的准备步骤：复制 .env, docs/，再运行 npm ci。命令要人采用后才会执行。')
    expect(systemText(t, 'Lead wrote down how a new worktree is got ready: run make. A person adopts the command before it runs.')).toBe(
      'Lead 写好了新工作区的准备步骤：运行 make。命令要人采用后才会执行。',
    )
  })

  it('says whose work went on the main line', () => {
    expect(systemText(t, "Merged Coder's work into the main line as abc1234: Add the feature")).toBe('Coder 的改动已合并到主线（abc1234）：Add the feature')
    expect(systemNote(t, "Merged Coder's work, which had Tester's and Reviewer's in it, into the main line as abc1234: Add tags")).toEqual({
      kind: 'merged',
      text: 'Coder 的改动（含 Tester、Reviewer 的）已合并到主线（abc1234）：Add tags',
      who: ['Coder', 'Tester', 'Reviewer'],
    })
    expect(
      systemText(
        t,
        "Reset Reviewer's branch veyloom/reviewer to the main line; its work is archived as refs/veyloom/set-aside/veyloom/reviewer/20260925T101500.000Z.",
      ),
    ).toBe('已把 Reviewer 的分支 veyloom/reviewer 重置到主线，原内容归档为 refs/veyloom/set-aside/veyloom/reviewer/20260925T101500.000Z')
    // Chats from before the wording changed still read.
    expect(
      systemText(
        t,
        "Set aside Reviewer's work, kept in git as refs/veyloom/set-aside/veyloom/reviewer/20260925T101500.000Z: its worktree starts over from the main line.",
      ),
    ).toBe('已把 Reviewer 的分支重置到主线，原内容归档为 refs/veyloom/set-aside/veyloom/reviewer/20260925T101500.000Z')
    // A note of old had the whole message: its first line is enough.
    expect(systemText(t, "Merged Coder's work into the main line as abc1234: Add tags\n\n- Parse them\n- Test them")).toBe(
      'Coder 的改动已合并到主线（abc1234）：Add tags',
    )
  })

  it('says which members changed the same files', () => {
    expect(systemText(t, 'Tester and Coder both changed README.md: whichever is merged second may conflict.')).toBe(
      'Tester、Coder 都改了 README.md，后合并的那个可能会冲突',
    )
    expect(systemText(t, 'Coder, Tester and Writer all changed a.go, b.go: whichever is merged second may conflict.')).toBe(
      'Coder、Tester、Writer 都改了 a.go、b.go，后合并的那个可能会冲突',
    )
    expect(systemText(t, 'A and B both changed 1, 2, 3, 4, 5 and 2 more: whichever is merged second may conflict.')).toBe(
      'A、B 都改了 1、2、3、4、5 等 7 个文件，后合并的那个可能会冲突',
    )
  })

  it('says the work a member handed on is done, and goes back to it', () => {
    expect(systemText(t, 'The work Planner handed on is done (Coder, Tester); back to Planner.')).toBe(
      'Coder、Tester 做完了 Planner 交出去的活，交回给 Planner',
    )
    // As the hub first said it.
    expect(systemText(t, 'The work Lead handed on is done (Coder); Lead sums it up.')).toBe('Coder 做完了 Lead 交出去的活，交回给 Lead')
    expect(systemNote(t, 'The work Lead handed on is done (Coder); back to Lead.')).toEqual({
      kind: 'sumUp',
      text: 'Coder 做完了 Lead 交出去的活，交回给 Lead',
      who: ['Coder'],
    })
  })

  it("says what a person committed in the project's checkout", () => {
    expect(systemNote(t, "Committed the changes in the project's checkout as 1a2b3c4: Say who leads")).toEqual({
      kind: 'committed',
      text: '仓库目录里的改动已提交（1a2b3c4）：Say who leads',
      who: [],
    })
  })

  it('says a wake was held back, and why', () => {
    expect(
      systemText(t, '@alice Ping mentioned Pong, but agents have woken 30 turns in this piece of work since a person last spoke; it waits for a person now.'),
    ).toBe('Ping 想叫醒 Pong，但这件事自你上次说话以来 agent 已经互相叫醒了 30 轮，等你决定')
    expect(systemText(t, 'Pong mentioned Ping, but the last 3 turns agents woke in this piece of work only talked; it waits for a person now.')).toBe(
      'Pong 想叫醒 Ping，但最近 3 轮被叫醒的都只说话、没干活，等你决定',
    )
  })

  it('says how a merge a member left under way was seen to', () => {
    expect(systemText(t, 'Coder settled the conflicts but left the merge uncommitted; Veyloom committed it as abc1234.')).toBe(
      'Coder 解决了冲突但没有提交，Veyloom 替它完成了合并（abc1234）',
    )
    expect(systemText(t, "Coder's worktree is still in the middle of a merge: README.md, a.go still have conflict markers.")).toBe(
      'Coder 的工作区还停在合并中：README.md、a.go 还有冲突标记',
    )
  })

  it('says a worktree could not be got ready, from the note to the leader', () => {
    const body = "@Lead getting Coder's worktree ready failed: the setup command failed: exit status 3.\n\n```\nno compiler\n```\n\nSet the steps right."
    expect(systemText(t, body)).toBe('给 Coder 准备工作区失败：the setup command failed: exit status 3。已叫 Lead 修改准备步骤。')
  })
})

describe('systemNote of a turn that said nothing', () => {
  it('says so, of whom', () => {
    expect(systemNote(t, 'Mute ended its turn without a word in this topic.')).toEqual({
      kind: 'silent',
      text: 'Mute 这一轮没在这个话题里留下回话',
      who: ['Mute'],
    })
  })
})

describe('systemNote of a turn cancelled or failed', () => {
  it('says whose turn was cancelled, and whether its next starts a new session', () => {
    expect(systemNote(t, "Slow's turn was cancelled")).toEqual({ kind: 'cancelled', text: 'Slow 的这一轮已取消', who: ['Slow'] })
    expect(systemText(t, "Slow's turn was cancelled; its next turn starts a new session")).toBe('Slow 的这一轮已取消，下一轮从新会话开始')
    expect(systemNote(t, "Slow's next turn starts a new session, as a person asked.")).toMatchObject({
      kind: 'session',
      text: 'Slow 下一轮从新会话开始（有人要求）',
    })
  })

  it('says whose turn failed, in the words the UI has for the failure', () => {
    expect(systemNote(t, 'Codex Implementer failed: scripted failure')).toEqual({
      kind: 'failed',
      text: 'Codex Implementer 这一轮失败了：scripted failure',
      who: ['Codex Implementer'],
    })
    // A leader's note that a worktree could not be got ready stays its own.
    expect(systemNote(t, "@Lead getting Coder's worktree ready failed: exit 1.").text).not.toContain('这一轮失败了')
  })
})

describe("systemNote of a member's new session", () => {
  const note = (reason: string) => `Rev started a new session (${reason}) and no longer remembers its earlier turns; the chat history is still here for it.`

  it('says who starts over, and why', () => {
    expect(systemNote(t, note('its role card changed'))).toEqual({
      kind: 'session',
      text: 'Rev 开了新会话（角色卡改了），之前几轮的事它不记得了，聊天记录都还在',
      who: ['Rev'],
    })
    expect(systemText(t, note('its working directory changed'))).toBe('Rev 开了新会话（工作目录变了），之前几轮的事它不记得了，聊天记录都还在')
    expect(systemText(t, note("it outgrew the model's context window"))).toContain('（超出了模型的上下文）')
  })

  it('leaves a reason it does not know as the hub wrote it', () => {
    expect(systemNote(t, note('the moon was full'))).toEqual({ kind: 'other', text: note('the moon was full'), who: [] })
  })
})

describe('noticeText', () => {
  it('says the hub’s notices of a worktree being got ready in the UI’s words', () => {
    expect(noticeText(t, "Waiting for the project's leader to set the project up for worktrees.")).toBe('等组长为工作区初始化项目')
    expect(noticeText(t, "Making Coder's worktree, on the branch veyloom/coder.")).toBe('正在给 Coder 建工作区，分支 veyloom/coder')
    expect(noticeText(t, 'Getting the worktree ready: copy .env, then run npm ci.')).toBe('正在准备工作区：复制 .env，再运行 npm ci')
    // A runtime's own are left as they are.
    expect(noticeText(t, 'rate limits are close')).toBe('rate limits are close')
  })

  it('reads as done once past', () => {
    expect(noticeText(t, "Making Coder's worktree, on the branch veyloom/coder.", true)).toBe('给 Coder 建了工作区，分支 veyloom/coder')
    expect(noticeText(t, 'Getting the worktree ready: run npm ci.', true)).toBe('准备好了工作区：运行 npm ci')
    expect(noticeText(t, "Waiting for the project's leader to set the project up for worktrees.", true)).toBe('组长为工作区初始化了项目')
  })
})

// What keeps a member from answering, in the UI's words (docs/design.md
// 5.23.3): the hub's times shown in the reader's zone.
describe('systemNote of a pause', () => {
  const at = '2026-09-27T08:23:00Z'
  const time = formatTime(at)

  it('says what the member waits for, and until when', () => {
    expect(systemNote(t, `Slow waits for claude's usage limit on laptop to reset at ${at}.`)).toEqual({
      kind: 'paused',
      text: `Slow 在等 laptop 上 Claude Code 的额度 ${time} 恢复，到时接着答。`,
      who: ['Slow'],
    })
    expect(systemText(t, "Slow waits: pi's usage limit on laptop is reached; resume it once the account has more.")).toBe(
      'Slow 在等：laptop 上 Pi 的额度用完了，续上后点「继续」。',
    )
    expect(systemText(t, 'Slow waits: codex is signed out on build box; sign it in again there, then resume it.')).toBe(
      'Slow 在等：build box 上的 Codex 登录失效了，到那台机器上重新登录后点「继续」。',
    )
    expect(systemText(t, `Slow waits: claude on laptop was turned down for too many requests; it tries again at ${at}.`)).toBe(
      `Slow 在等：laptop 上的 Claude Code 请求太多被限流，${time} 再试。`,
    )
    expect(systemText(t, `Slow waits: codex's service failed on laptop; it tries again at ${at}.`)).toBe(
      `Slow 在等：laptop 上 Codex 的服务出错了，${time} 再试。`,
    )
    expect(systemText(t, 'Broken waits: its last 3 turns failed; resume it once what failed is seen to.')).toBe(
      'Broken 连续 3 轮失败，已暂停；查明原因后点「继续」。',
    )
  })
})

// A member's reminder to itself (docs/design.md 5.23.4).
describe('systemNote of a reminder', () => {
  const due = '2026-09-28T09:00:00+08:00'

  it('says who set one, for when, and what for', () => {
    // In the page's language, which each test sets.
    const time = formatTime(due)
    expect(systemNote(t, `Slow set a reminder for ${due}: check how CI went`)).toEqual({
      kind: 'reminder',
      text: `Slow 定了提醒：${time}，check how CI went`,
      who: ['Slow'],
    })
    expect(isReminderNote(`Slow set a reminder for ${due}: check how CI went`)).toBe(true)
    expect(isReminderNote(`Slow's reminder, due ${due}: check how CI went`)).toBe(false)
  })

  it('says it came due, and how late when it was', () => {
    expect(systemNote(t, `Slow's reminder, due ${due}: check how CI went\nand the logs`)).toEqual({
      kind: 'reminderDue',
      text: 'Slow 的提醒到了：check how CI went\nand the logs',
      who: ['Slow'],
    })
    expect(systemText(t, `Slow's reminder, due ${due} (1d2h10m late): check`)).toBe('Slow 的提醒到了，晚了 1 天 2 小时 10 分：check')
  })

  it('says its wake was held back, and why', () => {
    const limit = "@alice Slow's reminder came due, but agents have woken 30 turns in this piece of work since a person last spoke; it waits for a person now."
    expect(systemNote(t, limit)).toEqual({
      kind: 'hold',
      text: 'Slow 的提醒到了，但这件事自你上次说话以来 agent 已经互相叫醒了 30 轮，等你决定',
      who: ['Slow'],
    })
    expect(systemText(t, "Slow's reminder came due, but the last 3 turns agents woke in this piece of work only talked; it waits for a person now.")).toBe(
      'Slow 的提醒到了，但最近 3 轮被叫醒的都只说话、没干活，等你决定',
    )
    const people = "@alice Slow's reminder came due, but only people wake members in this project; it waits for a person now."
    expect(systemText(t, people)).toBe('Slow 的提醒到了；这个项目只由人叫醒成员，等你决定')
    expect(isHoldNote(limit) && isHoldNote(people)).toBe(true)
  })

  it("reads the hub's spans", () => {
    expect(spanMs('45m')).toBe(45 * 60_000)
    expect(spanMs('2d4h')).toBe(52 * 3_600_000)
    expect(spanMs('')).toBe(0)
    expect(spanMs('soon')).toBe(0)
  })
})
