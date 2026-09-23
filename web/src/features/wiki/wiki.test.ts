import { describe, expect, it } from 'vitest'
import { resolvePage, wikiRoute } from './links'
import { withoutTitleHeading } from './body'
import { actorName, reasonOf } from './names'

describe('wiki links', () => {
  it('reads the part of the URL after /wiki', () => {
    expect(wikiRoute('')).toEqual({ kind: 'overview' })
    expect(wikiRoute('changes')).toEqual({ kind: 'changes' })
    expect(wikiRoute('decisions/payload-json.md')).toEqual({ kind: 'page', path: '/decisions/payload-json.md' })
    expect(wikiRoute('decisions/')).toEqual({ kind: 'overview' })
  })

  it('resolves links in a page the way OKF does', () => {
    expect(resolvePage('/facts/port.md', '/modules/config.md')).toBe('/modules/config.md')
    expect(resolvePage('/facts/port.md', '../modules/config.md#top')).toBe('/modules/config.md')
    expect(resolvePage('/facts/port.md', 'other.md')).toBe('/facts/other.md')
    expect(resolvePage('/facts/port.md', '../../outside.md')).toBeUndefined()
    expect(resolvePage('/facts/port.md', 'https://example.com/a.md')).toBeUndefined()
    expect(resolvePage('/facts/port.md', '#section')).toBeUndefined()
    expect(resolvePage('/facts/port.md', 'picture.png')).toBeUndefined()
    expect(resolvePage('/facts/port.md', '%E4%B8%AD.md')).toBe('/facts/中.md')
  })
})

describe('wiki names', () => {
  it('names actors the way people call them', () => {
    expect(actorName('human:alice')).toBe('alice')
    expect(actorName('process:veyloom')).toBe('Veyloom')
    expect(actorName('codex/default')).toBe('Codex')
    expect(actorName('claude-code/haiku')).toBe('Claude Code · haiku')
    expect(actorName('pi/deepseek/deepseek-chat')).toBe('Pi · deepseek/deepseek-chat')
  })

  it('guesses a type from its folder', () => {})
})

describe('commit subjects', () => {
  it("reads the hub's subjects in the UI's words", async () => {
    const { subjectText } = await import('./names')
    const { t } = await import('@/lib/i18n')
    expect(subjectText(t, 'Confirmed: 审批的 payload')).toBe('确认「审批的 payload」')
    expect(subjectText(t, 'Writer in topic #3')).toBe('Writer 在话题 #3 的改动')
    expect(subjectText(t, 'Undo 1234567: Resident: 命名')).toBe('撤回：把「命名」设为常驻')
    expect(subjectText(t, 'Something else')).toBe('Something else')
    expect(subjectText(t, 'Editor of docs-site in topic #2')).toBe('Editor（docs-site）在话题 #2 的改动')
    expect(subjectText(t, 'Undo 1234567: Handed over to docs-site: Go 表格驱动测试')).toBe('撤回：转交「Go 表格驱动测试」')
    expect(subjectText(t, 'Keep go-table-tests after its trial')).toBe('go-table-tests 试用期满，转正')
    expect(subjectText(t, 'Roll back go-table-tests to 1234567')).toBe('退回 go-table-tests 的改动')
    expect(subjectText(t, 'Imported the skill release-notes from /Users/me/.claude/skills/release-notes')).toBe('导入技能 release-notes')
  })

  it('say what the lines for the pages leave out', async () => {
    const { commitNote } = await import('./names')
    const { t } = await import('@/lib/i18n')
    const teams = [{ slug: 'docs-site', project_id: 'p2', name: '文档站', room_id: 'r2' }]
    expect(commitNote(t, 'Handed over to docs-site: Go 表格驱动测试', teams)).toBe('转给「文档站」团队')
    expect(commitNote(t, 'Handed over to gone: Go 表格驱动测试', teams)).toBe('转给「gone」团队')
    expect(commitNote(t, 'Left without a team: the project docs-site is gone')).toBe('团队 docs-site 的项目已经删除，技能改为无人负责')
    expect(commitNote(t, 'Regenerate the indexes')).toBe('重新生成了目录')
    expect(commitNote(t, 'Confirmed: Go 表格驱动测试')).toBe('')
  })
})

describe('what the log says', () => {
  it('keeps why a person or an agent changed something', () => {
    expect(reasonOf('undid 1234567 (Decider in topic #2) by human:alice: 还没定下来')).toBe('还没定下来')
    expect(reasonOf('undid 1234567 (Resident: a) by human:alice')).toBeUndefined()
    expect(reasonOf('[Table tests](/skills/t/SKILL.md) rolled back to abc1234 by claude-code/haiku: 名字太长')).toBe('名字太长')
    expect(reasonOf('[Table tests](/skills/t/SKILL.md) by process:skill-trial: kept after 3 turns used it')).toBeUndefined()
    // A page changed or deprecated, moved, or changed with no reason given.
    expect(reasonOf('[审批的 payload 用 json](/decisions/a.md) by codex/default: 改成 jsonb 了')).toBe('改成 jsonb 了')
    expect(reasonOf('[A](/facts/b.md) was /facts/a.md by human:alice: 合并')).toBe('合并')
    expect(reasonOf('[A](/facts/a.md) by codex/default')).toBeUndefined()
  })
})

describe('a page read', () => {
  it('leaves out a heading that only says the title again', () => {
    expect(withoutTitleHeading('# Table tests\n\nWrite the cases as a table.\n', 'Table tests')).toBe('Write the cases as a table.\n')
    expect(withoutTitleHeading('\n#  Table tests  #\nBody', 'Table tests')).toBe('Body')
    expect(withoutTitleHeading('# Another\n\nBody', 'Table tests')).toBe('# Another\n\nBody')
    expect(withoutTitleHeading('## Table tests\n\nBody', 'Table tests')).toBe('## Table tests\n\nBody')
    expect(withoutTitleHeading('Body', '')).toBe('Body')
  })
})
