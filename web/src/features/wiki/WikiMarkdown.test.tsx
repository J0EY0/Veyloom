import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { WikiMarkdown } from './WikiMarkdown'

// A file the project's wiki keeps is read from the hub (docs/design.md
// 5.16): a picture shown in place, anything else opened from there.
describe('WikiMarkdown', () => {
  it('reads the files a page keeps from the hub', () => {
    renderWithProviders(
      <WikiMarkdown
        text={'![模块边界](/files/module-map/file.png)\n\n[发版清单](../files/release/checklist.pdf) 和 [另一页](/facts/x.md)'}
        from="/modules/module-map.md"
        space={{ kind: 'project', projectId: 'p1', roomId: 'r1' }}
      />,
    )
    expect(screen.getByRole('img', { name: '模块边界' })).toHaveAttribute('src', '/api/v1/projects/p1/wiki/file?path=%2Ffiles%2Fmodule-map%2Ffile.png')
    expect(screen.getByRole('link', { name: '发版清单' })).toHaveAttribute('href', '/api/v1/projects/p1/wiki/file?path=%2Ffiles%2Frelease%2Fchecklist.pdf')
    expect(screen.getByRole('link', { name: '另一页' })).toHaveAttribute('href', '/rooms/r1/wiki/facts/x.md')
  })
})
