import { describe, expect, it } from 'vitest'
import { t } from '@/lib/i18n'
import { isDraftNote } from './draftNote'
import { systemNote, systemText } from './systemNote'

// What members draft for a person, and what came of it, in the UI's words
// (docs/design.md 5.23.5).
describe('systemNote of a draft', () => {
  it('names the card of each kind', () => {
    const merge = "Lead drafted putting Coder's work on the main line, as: Add tags\n\nWith tests."
    expect(systemNote(t, merge)).toEqual({ kind: 'draft', text: 'Lead 起草：把 Coder 的活合进主线', who: ['Lead'] })
    expect(systemText(t, "Lead drafted giving Coder's work up, since: wrong approach")).toBe('Lead 起草：放弃 Coder 分支上的活')
    expect(systemText(t, 'Lead drafted installing the skill go-testing for Coder.')).toBe('Lead 起草：给 Coder 装上技能 go-testing')
    expect(isDraftNote(merge)).toBe(true)
    expect(isDraftNote("Merged Coder's work into the main line as abc1234: Add tags")).toBe(false)
  })

  it('says what came of it, and what the member goes on to', () => {
    expect(systemNote(t, "alice put Coder's work on the main line as Lead drafted, as commit abc1234. Then, Lead said: hand the tests to Tester")).toEqual({
      kind: 'merged',
      text: 'alice 按 Lead 的起草把 Coder 的活合进了主线（abc1234）；Lead 接着：hand the tests to Tester',
      who: ['alice'],
    })
    expect(
      systemText(
        t,
        "alice tried putting Coder's work on the main line, as Lead drafted, but it conflicts with the main line in README.md, src/api.ts; nothing changed. Then, Lead said: have Coder settle them",
      ),
    ).toBe('alice 按 Lead 的起草合并 Coder 的活，和主线在 README.md、src/api.ts 有冲突，没有改动；Lead 接着：have Coder settle them')
    expect(systemText(t, "alice gave Coder's work up as Lead drafted; it is kept as refs/veyloom/set-aside/x. Then, Lead said: start over")).toBe(
      'alice 按 Lead 的起草放弃了 Coder 分支上的活，归档在 refs/veyloom/set-aside/x；Lead 接着：start over',
    )
    expect(systemNote(t, 'alice installed the skill go-testing for Coder as Lead drafted. Then, Lead said: ask Coder for the tests').kind).toBe('installed')
  })
})
