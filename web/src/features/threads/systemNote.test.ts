import { describe, expect, it } from 'vitest'
import { t } from '@/lib/i18n'
import { noticeText, systemNote, systemText } from './systemNote'

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
