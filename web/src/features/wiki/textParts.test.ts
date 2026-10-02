import { describe, expect, it } from 'vitest'
import { contextParts, markParts } from './textParts'

describe('markParts', () => {
  it('marks every word of the search, ignoring case, overlapping ones as one', () => {
    expect(markParts('Tags are lowercase: TAG, tags.', 'tag')).toEqual([
      { text: 'Tag', hit: true },
      { text: 's are lowercase: ', hit: false },
      { text: 'TAG', hit: true },
      { text: ', ', hit: false },
      { text: 'tag', hit: true },
      { text: 's.', hit: false },
    ])
    expect(markParts('笔记的标签在存入时统一转为小写', '标签 存入')).toEqual([
      { text: '笔记的', hit: false },
      { text: '标签', hit: true },
      { text: '在', hit: false },
      { text: '存入', hit: true },
      { text: '时统一转为小写', hit: false },
    ])
    expect(markParts('lowercase', 'lower wercase')).toEqual([{ text: 'lowercase', hit: true }])
  })

  it('leaves the text whole with nothing searched', () => {
    expect(markParts('标签', '  ')).toEqual([{ text: '标签', hit: false }])
  })
})

describe('contextParts', () => {
  it('reads links as their text, a page by its title', () => {
    const titles: Record<string, string> = { '/decisions/tag-lowercase.md': '标签一律小写' }
    expect(contextParts('标签小写规则（见 [/decisions/tag-lowercase.md]）只在写入时执行。', (path) => titles[path])).toEqual([
      { text: '标签小写规则（见 ', hit: false },
      { text: '标签一律小写', hit: true },
      { text: '）只在写入时执行。', hit: false },
    ])
    expect(contextParts('[标签功能实现（Topic #1)] 中描述。', () => undefined)).toEqual([
      { text: '标签功能实现（Topic #1)', hit: true },
      { text: ' 中描述。', hit: false },
    ])
  })
})
